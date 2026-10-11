package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"botkeep/internal/models"
)

type Engine struct {
	db       *models.DB
	client   *http.Client
	interval time.Duration
	mu       sync.Mutex
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
		db:       db,
		client:   client,
		interval: interval,
	}
}

// Start launches the background prober loop until context cancellation.
func (e *Engine) Start(ctx context.Context) {
	log.Printf("[Monitor] Background health check prober started (polling every %v)", e.interval)

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
		if s.Driver == "http" || s.Driver == "demo" {
			wg.Add(1)
			go func(srv models.Server) {
				defer wg.Done()
				_ = e.ProbeServer(ctx, srv)
			}(s)
		}
	}
	wg.Wait()
}

// ProbeServer performs a health check probe against a single node.
func (e *Engine) ProbeServer(ctx context.Context, s models.Server) error {
	switch s.Driver {
	case "http":
		if s.EndpointURL == "" {
			return nil
		}

		req, err := http.NewRequestWithContext(ctx, "GET", s.EndpointURL, nil)
		if err != nil {
			_ = e.db.RecordProbe(s.ID, 0, 0, false, fmt.Sprintf("Invalid URL: %v", err))
			return err
		}
		req.Header.Set("User-Agent", "BotKeep-Fleet-Prober/1.2")

		start := time.Now()
		resp, err := e.client.Do(req)
		latency := float64(time.Since(start).Microseconds()) / 1000.0

		if err != nil {
			_ = e.db.RecordProbe(s.ID, latency, 0, false, err.Error())
			return err
		}
		defer resp.Body.Close()

		success := resp.StatusCode >= 200 && resp.StatusCode < 400
		errMsg := ""
		if !success {
			errMsg = fmt.Sprintf("HTTP status %d", resp.StatusCode)
		}

		return e.db.RecordProbe(s.ID, latency, resp.StatusCode, success, errMsg)

	case "demo":
		// Light jitter for demo nodes
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
