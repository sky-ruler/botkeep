package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"botkeep/internal/models"
)

type Engine struct {
	db         *models.DB
	client     *http.Client
	interval   time.Duration
	webhookURL string
	dispatcher *Dispatcher
	mu         sync.Mutex
}

func NewEngine(db *models.DB, interval time.Duration) *Engine {
	if interval <= 0 {
		interval = 15 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		MaxIdleConns:          100,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	return &Engine{
		db:         db,
		client:     client,
		interval:   interval,
		webhookURL: os.Getenv("BOTKEEP_WEBHOOK_URL"),
		dispatcher: NewDispatcher(5 * time.Second),
	}
}

// GetDispatcher returns the notification dispatcher.
func (e *Engine) GetDispatcher() *Dispatcher {
	return e.dispatcher
}

// Start launches the background prober loop until context cancellation.
func (e *Engine) Start(ctx context.Context) {
	log.Printf("[Monitor] Background multi-protocol prober started (polling every %v)", e.interval)

	// Immediate initial sweep
	go e.runProbeSweep(ctx)

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Monitor] Background prober shut down gracefully.")
			return
		case <-ticker.C:
			e.runProbeSweep(ctx)
		}
	}
}

func (e *Engine) runProbeSweep(ctx context.Context) {
	servers, err := e.db.GetAllServers()
	if err != nil {
		log.Printf("[Monitor] Failed to list servers for probe sweep: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, s := range servers {
		if s.Driver == "http" || s.Driver == "tcp" || s.Driver == "demo" {
			wg.Add(1)
			go func(srv models.Server) {
				defer wg.Done()
				_ = e.ProbeServer(ctx, srv)
			}(s)
		}
	}
	wg.Wait()
}

// ProbeServer performs a health check probe against a single node (HTTP, HTTPS, TCP, or Simulated).
func (e *Engine) ProbeServer(ctx context.Context, s models.Server) error {
	switch s.Driver {
	case "http":
		if s.EndpointURL == "" {
			return nil
		}

		req, err := http.NewRequestWithContext(ctx, "GET", s.EndpointURL, nil)
		if err != nil {
			_ = e.db.RecordProbe(s.ID, 0, 0, false, fmt.Sprintf("Invalid URL: %v", err))
			e.dispatchAlert(s, false, err.Error())
			return err
		}
		req.Header.Set("User-Agent", "BotKeep-Fleet-Prober/2.0")

		start := time.Now()
		resp, err := e.client.Do(req)
		latency := float64(time.Since(start).Microseconds()) / 1000.0
		if latency <= 0 {
			latency = 0.1
		}

		if err != nil {
			_ = e.db.RecordProbe(s.ID, latency, 0, false, err.Error())
			e.dispatchAlert(s, false, err.Error())
			return err
		}
		defer resp.Body.Close()

		// Extract TLS certificate expiration if HTTPS
		if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			cert := resp.TLS.PeerCertificates[0]
			daysRemaining := int(time.Until(cert.NotAfter).Hours() / 24)
			_ = e.db.UpdateSSLExpiry(s.ID, daysRemaining)
		}

		success := resp.StatusCode >= 200 && resp.StatusCode < 400
		errMsg := ""
		if !success {
			errMsg = fmt.Sprintf("HTTP status %d", resp.StatusCode)
			e.dispatchAlert(s, false, errMsg)
		} else if s.Status == "stopped" {
			e.dispatchAlert(s, true, fmt.Sprintf("HTTP %d OK", resp.StatusCode))
		}

		return e.db.RecordProbe(s.ID, latency, resp.StatusCode, success, errMsg)

	case "tcp":
		if s.EndpointURL == "" {
			return nil
		}

		start := time.Now()
		var d net.Dialer
		d.Timeout = 4 * time.Second
		conn, err := d.DialContext(ctx, "tcp", s.EndpointURL)
		latency := float64(time.Since(start).Microseconds()) / 1000.0
		if latency <= 0 {
			latency = 0.1
		}

		if err != nil {
			_ = e.db.RecordProbe(s.ID, latency, 0, false, err.Error())
			e.dispatchAlert(s, false, err.Error())
			return err
		}
		_ = conn.Close()

		if s.Status == "stopped" {
			e.dispatchAlert(s, true, "TCP socket connected")
		}

		return e.db.RecordProbe(s.ID, latency, 200, true, "")

	case "demo":
		// Realistic jitter for simulated demo nodes
		lat := s.LatencyMS
		if s.Status == "active" {
			lat += (float64(time.Now().UnixNano()%20) - 10) / 10.0
			if lat < 5 {
				lat = 5
			}
			return e.db.RecordProbe(s.ID, lat, 200, true, "")
		}
		return nil

	default:
		return nil
	}
}

// ProbeNow immediately triggers an on-demand probe for a specific server.
func (e *Engine) ProbeNow(ctx context.Context, serverID string) (*models.Server, error) {
	s, err := e.db.GetServerByID(serverID)
	if err != nil {
		return nil, err
	}

	_ = e.ProbeServer(ctx, *s)
	return e.db.GetServerByID(serverID)
}

// dispatchAlert logs incident events and dispatches notifications to configured channels.
func (e *Engine) dispatchAlert(s models.Server, recovered bool, reason string) {
	if !recovered {
		// Multi-Region Consensus Probing Shield:
		// If 2 or more satellite regions are reporting, suppress false alarms if only 1 region fails
		isConsensusDown, failingRegions, totalRegions, err := e.db.EvaluateConsensus(s.ID)
		if err == nil && totalRegions >= 2 && !isConsensusDown {
			shieldMsg := fmt.Sprintf("[%s] CONSENSUS SHIELD: Regional failure (%s) suppressed (only %d/%d regions affected). False alarm eliminated.", s.Name, reason, len(failingRegions), totalRegions)
			_ = e.db.AddLog(s.ID, "WARN", shieldMsg)
			log.Println("[Consensus Shield]", shieldMsg)
			return
		} else if isConsensusDown && len(failingRegions) >= 2 {
			reason = fmt.Sprintf("%s [Verified by %d-region consensus: %s]", reason, len(failingRegions), strings.Join(failingRegions, ", "))
		}
	}

	level := "ERROR"
	msg := fmt.Sprintf("[%s] INCIDENT: Health check probe failed (%s)", s.Name, reason)
	if recovered {
		level = "INFO"
		msg = fmt.Sprintf("[%s] RECOVERY: Service operational (%s)", s.Name, reason)
	}

	_ = e.db.AddLog(s.ID, level, msg)

	ev := AlertEvent{
		ServerID:    s.ID,
		ServerName:  s.Name,
		Driver:      s.Driver,
		EndpointURL: s.EndpointURL,
		Region:      s.Region,
		Status:      s.Status,
		Recovered:   recovered,
		Reason:      reason,
		LatencyMS:   s.LatencyMS,
		Timestamp:   time.Now(),
	}

	// 1. Dispatch across all enabled notification channels
	channels, err := e.db.GetActiveNotificationChannels()
	if err == nil {
		for _, ch := range channels {
			events := strings.ToLower(ch.Events)
			shouldSend := false
			if events == "all" || strings.Contains(events, "all") {
				shouldSend = true
			} else if !recovered && (strings.Contains(events, "down") || strings.Contains(events, "incident")) {
				shouldSend = true
			} else if recovered && strings.Contains(events, "recovery") {
				shouldSend = true
			}

			if !shouldSend {
				continue
			}

			go func(channel models.NotificationChannel) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := e.dispatcher.Dispatch(ctx, channel, ev); err != nil {
					_ = e.db.AddLog(s.ID, "WARN", fmt.Sprintf("[%s] Notification failed to %s (%s): %v", s.Name, channel.Name, channel.ChannelType, err))
				} else {
					_ = e.db.UpdateNotificationChannelLastDispatched(channel.ID)
					_ = e.db.AddLog(s.ID, "INFO", fmt.Sprintf("[%s] Notification delivered to %s (%s)", s.Name, channel.Name, channel.ChannelType))
				}
			}(ch)
		}
	}

	// 2. Legacy fallback webhook if BOTKEEP_WEBHOOK_URL is set
	if e.webhookURL != "" {
		go func(targetURL string) {
			payload := map[string]any{
				"event":     "node_alert",
				"server_id": s.ID,
				"name":      s.Name,
				"driver":    s.Driver,
				"endpoint":  s.EndpointURL,
				"status":    s.Status,
				"recovered": recovered,
				"reason":    reason,
				"timestamp": time.Now().Format(time.RFC3339),
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return
			}

			req, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "BotKeep-Alert-Webhook/2.0")

			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}(e.webhookURL)
	}
}
