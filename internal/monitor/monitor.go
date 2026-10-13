package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
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
	healer     *Healer
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
		healer:     NewHealer(db, 8*time.Second),
	}
}

// GetDispatcher returns the notification dispatcher.
func (e *Engine) GetDispatcher() *Dispatcher {
	return e.dispatcher
}

// GetHealer returns the active remediation healer.
func (e *Engine) GetHealer() *Healer {
	return e.healer
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
		if s.Driver == "http" || s.Driver == "tcp" || s.Driver == "grpc" || s.Driver == "dns" || s.Driver == "demo" {
			wg.Add(1)
			go func(srv models.Server) {
				defer wg.Done()
				_ = e.ProbeServer(ctx, srv)
			}(s)
		}
	}
	wg.Wait()
}

// ProbeServer performs a health check probe against a single node (HTTP, HTTPS, TCP, gRPC, DNS, or Simulated).
func (e *Engine) ProbeServer(ctx context.Context, s models.Server) error {
	switch s.Driver {
	case "http":
		if s.EndpointURL == "" && s.ScenarioSteps == "" {
			return nil
		}

		// Multi-step synthetic scenario execution if defined
		if s.ScenarioSteps != "" {
			if steps, err := ParseScenarioSteps(s.ScenarioSteps); err == nil && len(steps) > 0 {
				scenRes := ExecuteScenario(ctx, steps, 15*time.Second)
				if !scenRes.Passed {
					_ = e.db.RecordProbe(s.ID, scenRes.TotalLatency, 500, false, scenRes.ErrorMessage)
					e.dispatchAlert(s, false, scenRes.ErrorMessage)
					return fmt.Errorf("scenario failed: %s", scenRes.ErrorMessage)
				}
				if s.Status == "stopped" {
					e.dispatchAlert(s, true, fmt.Sprintf("All %d synthetic scenario steps passed", scenRes.TotalSteps))
				}
				return e.db.RecordProbe(s.ID, scenRes.TotalLatency, 200, true, "")
			}
		}

		if s.EndpointURL == "" {
			return nil
		}

		method := strings.ToUpper(strings.TrimSpace(s.HTTPMethod))
		if method == "" {
			method = "GET"
		}

		req, err := http.NewRequestWithContext(ctx, method, s.EndpointURL, nil)
		if err != nil {
			_ = e.db.RecordProbe(s.ID, 0, 0, false, fmt.Sprintf("Invalid URL: %v", err))
			e.dispatchAlert(s, false, err.Error())
			return err
		}
		req.Header.Set("User-Agent", "BotKeep-Fleet-Prober/2.0")

		if s.CustomHeaders != "" {
			parseAndApplyHeaders(req, s.CustomHeaders)
		}

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

		// Read up to 64KB for body assertion verification
		var bodyText string
		if s.BodyAssertion != "" {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			bodyText = string(bodyBytes)
		}

		// Deep SSL & DNS inspection for HTTPS endpoints
		if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			cert := resp.TLS.PeerCertificates[0]
			daysRemaining := int(time.Until(cert.NotAfter).Hours() / 24)
			issuerOrg := cert.Issuer.CommonName
			if len(cert.Issuer.Organization) > 0 {
				issuerOrg = strings.Join(cert.Issuer.Organization, ", ")
			}
			sans := strings.Join(cert.DNSNames, ", ")
			_ = e.db.UpdateSSLTelemetry(s.ID, daysRemaining, issuerOrg, sans, cert.NotAfter)
		}

		// Background DNS resolution check
		go func(srv models.Server) {
			dns := InspectDNS(context.Background(), srv.EndpointURL, 3*time.Second)
			if dns != nil && dns.Success && len(dns.IPs) > 0 {
				_ = e.db.UpdateDNSTelemetry(srv.ID, dns.LookupLatencyMS, strings.Join(dns.IPs, ", "))
			}
		}(s)

		statusMatch := CheckStatusMatch(resp.StatusCode, s.ExpectedStatus)
		bodyMatch := true
		if s.BodyAssertion != "" {
			bodyMatch = strings.Contains(bodyText, s.BodyAssertion)
		}

		success := statusMatch && bodyMatch
		errMsg := ""
		if !statusMatch {
			errMsg = fmt.Sprintf("HTTP status %d (expected %s)", resp.StatusCode, s.ExpectedStatus)
			e.dispatchAlert(s, false, errMsg)
		} else if !bodyMatch {
			errMsg = fmt.Sprintf("Body keyword %q missing in HTTP response", s.BodyAssertion)
			e.dispatchAlert(s, false, errMsg)
		} else if s.Status == "stopped" {
			e.dispatchAlert(s, true, fmt.Sprintf("HTTP %d OK", resp.StatusCode))
		}

		return e.db.RecordProbe(s.ID, latency, resp.StatusCode, success, errMsg)

	case "grpc":
		if s.EndpointURL == "" {
			return nil
		}

		probe := ProbeGRPC(ctx, s.EndpointURL, s.GRPCService, 4*time.Second)

		// Deep SSL check if GRPCS endpoint
		if strings.HasPrefix(s.EndpointURL, "https://") || strings.HasPrefix(s.EndpointURL, "grpcs://") || strings.HasSuffix(s.EndpointURL, ":443") {
			go func(srv models.Server) {
				if ssl, err := InspectSSL(context.Background(), srv.EndpointURL, 3*time.Second); err == nil && ssl != nil {
					_ = e.db.UpdateSSLTelemetry(srv.ID, ssl.DaysRemaining, ssl.Issuer, strings.Join(ssl.SANs, ", "), ssl.ValidTo)
				}
			}(s)
		}

		if !probe.Success {
			_ = e.db.RecordProbe(s.ID, probe.LatencyMS, probe.StatusCode, false, probe.ErrorMessage)
			e.dispatchAlert(s, false, probe.ErrorMessage)
			return fmt.Errorf("gRPC probe failed: %s", probe.ErrorMessage)
		}

		if s.Status == "stopped" {
			e.dispatchAlert(s, true, fmt.Sprintf("gRPC service %s OK", probe.ServingStatus))
		}

		return e.db.RecordProbe(s.ID, probe.LatencyMS, 200, true, "")

	case "dns":
		if s.EndpointURL == "" {
			return nil
		}

		dns := InspectDNS(ctx, s.EndpointURL, 4*time.Second)
		if !dns.Success {
			_ = e.db.RecordProbe(s.ID, dns.LookupLatencyMS, 0, false, dns.Error)
			e.dispatchAlert(s, false, dns.Error)
			return fmt.Errorf("DNS resolution failed: %s", dns.Error)
		}

		_ = e.db.UpdateDNSTelemetry(s.ID, dns.LookupLatencyMS, strings.Join(dns.IPs, ", "))

		if s.Status == "stopped" {
			e.dispatchAlert(s, true, fmt.Sprintf("DNS resolved %d records", len(dns.IPs)))
		}

		return e.db.RecordProbe(s.ID, dns.LookupLatencyMS, 200, true, "")

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

		// Trigger Active Self-Healing Runbook if configured
		if s.RunbookEnabled && s.RunbookURL != "" {
			go func(srv models.Server, failReason string) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = e.healer.TriggerRemediation(ctx, srv, failReason)
			}(s, reason)
		}
	} else {
		// Node recovered: resolve pending auto-healing remediations
		if s.RunbookEnabled {
			e.healer.OnRecovery(s)
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

func parseAndApplyHeaders(req *http.Request, raw string) {
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ';'
	})
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if k != "" {
				req.Header.Set(k, v)
			}
		}
	}
}

// CheckStatusMatch validates if the received HTTP status code satisfies the expectation expression.
// Supports: "200", "200,201", "2xx", "200-299", or comma-delimited combinations.
func CheckStatusMatch(code int, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return code >= 200 && code < 400
	}

	parts := strings.Split(expected, ",")
	for _, part := range parts {
		p := strings.TrimSpace(strings.ToLower(part))
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "xx") && len(p) == 3 {
			digit := int(p[0] - '0')
			if digit >= 1 && digit <= 5 {
				if code >= digit*100 && code < (digit+1)*100 {
					return true
				}
			}
			continue
		}
		if strings.Contains(p, "-") {
			rangeParts := strings.SplitN(p, "-", 2)
			minC, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			maxC, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 == nil && err2 == nil && code >= minC && code <= maxC {
				return true
			}
			continue
		}
		if expectedCode, err := strconv.Atoi(p); err == nil {
			if code == expectedCode {
				return true
			}
		}
	}
	return false
}
