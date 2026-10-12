package monitor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"botkeep/internal/models"
)

func setupTestDB(t *testing.T) (*models.DB, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "botkeep-monitor-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := models.NewDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
	}

	return db, cleanup
}

func TestProbeServerHTTP(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Mock HTTP target server returning 200 OK
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("healthy"))
	}))
	defer ts.Close()

	srv, err := db.CreateServer("mock-web-api", "us-east-1", "http", ts.URL)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	engine := NewEngine(db, time.Minute)
	err = engine.ProbeServer(context.Background(), *srv)
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}

	updated, err := db.GetServerByID(srv.ID)
	if err != nil {
		t.Fatalf("GetServerByID failed: %v", err)
	}

	if updated.Status != "active" {
		t.Errorf("Expected status active after 200 OK probe, got %s", updated.Status)
	}
	if updated.LatencyMS <= 0 {
		t.Errorf("Expected positive latency, got %f", updated.LatencyMS)
	}
	if updated.UptimePct != 100.0 {
		t.Errorf("Expected 100%% uptime SLA, got %f", updated.UptimePct)
	}
	if len(updated.ProbePoints) == 0 {
		t.Errorf("Expected probe history point recorded, got 0")
	}
}

func TestProbeServerHTTPFailure(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Mock HTTP server returning 503 Service Unavailable
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	srv, err := db.CreateServer("failing-service", "eu-central-1", "http", ts.URL)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	engine := NewEngine(db, time.Minute)
	_ = engine.ProbeServer(context.Background(), *srv)

	updated, err := db.GetServerByID(srv.ID)
	if err != nil {
		t.Fatalf("GetServerByID failed: %v", err)
	}

	if updated.Status != "stopped" {
		t.Errorf("Expected status stopped after 503 probe failure, got %s", updated.Status)
	}
}

func TestProbeNowOnDemand(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	srv, err := db.CreateServer("demo-probe-target", "ap-southeast-1", "demo", "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	engine := NewEngine(db, time.Minute)
	probed, err := engine.ProbeNow(context.Background(), srv.ID)
	if err != nil {
		t.Fatalf("ProbeNow failed: %v", err)
	}

	if probed == nil {
		t.Fatalf("Expected non-nil server result from ProbeNow")
	}
	if probed.ID != srv.ID {
		t.Errorf("Expected server ID %s, got %s", srv.ID, probed.ID)
	}
}

func TestProbeServerTCP(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Spin up real local TCP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen on tcp: %v", err)
	}
	defer ln.Close()

	// Handle connections in background
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	srv, err := db.CreateServer("redis-mock", "us-east-1", "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	engine := NewEngine(db, time.Minute)
	err = engine.ProbeServer(context.Background(), *srv)
	if err != nil {
		t.Fatalf("ProbeServer TCP failed: %v", err)
	}

	updated, err := db.GetServerByID(srv.ID)
	if err != nil {
		t.Fatalf("GetServerByID failed: %v", err)
	}

	if updated.Status != "active" {
		t.Errorf("Expected status active for reachable TCP port, got %s", updated.Status)
	}
	if updated.LatencyMS <= 0 {
		t.Errorf("Expected positive latency for TCP probe, got %f", updated.LatencyMS)
	}
}

func TestDispatcherChannels(t *testing.T) {
	var discordReceived, slackReceived, ntfyReceived, webhookReceived bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discord":
			discordReceived = true
		case "/slack":
			slackReceived = true
		case "/my-topic":
			ntfyReceived = true
			if r.Header.Get("Title") == "" {
				t.Errorf("Expected Title header on ntfy request")
			}
		case "/webhook":
			webhookReceived = true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer ts.Close()

	d := NewDispatcher(2 * time.Second)
	ctx := context.Background()

	ev := AlertEvent{
		ServerID:    "srv-test",
		ServerName:  "api-gateway",
		Driver:      "http",
		EndpointURL: "https://api.example.com",
		Region:      "us-east-1",
		Status:      "stopped",
		Recovered:   false,
		Reason:      "Connection refused (503)",
		LatencyMS:   120.5,
		Timestamp:   time.Now(),
	}

	// 1. Discord
	dCfg, _ := json.Marshal(models.DiscordConfig{WebhookURL: ts.URL + "/discord"})
	err := d.Dispatch(ctx, models.NotificationChannel{ChannelType: "discord", ConfigJSON: string(dCfg)}, ev)
	if err != nil {
		t.Fatalf("Discord dispatch failed: %v", err)
	}

	// 2. Slack
	sCfg, _ := json.Marshal(models.SlackConfig{WebhookURL: ts.URL + "/slack"})
	err = d.Dispatch(ctx, models.NotificationChannel{ChannelType: "slack", ConfigJSON: string(sCfg)}, ev)
	if err != nil {
		t.Fatalf("Slack dispatch failed: %v", err)
	}

	// 3. ntfy
	nCfg, _ := json.Marshal(models.NtfyConfig{Server: ts.URL, Topic: "my-topic"})
	err = d.Dispatch(ctx, models.NotificationChannel{ChannelType: "ntfy", ConfigJSON: string(nCfg)}, ev)
	if err != nil {
		t.Fatalf("ntfy dispatch failed: %v", err)
	}

	// 4. Webhook
	wCfg, _ := json.Marshal(models.WebhookConfig{URL: ts.URL + "/webhook", SecretToken: "sec-token"})
	err = d.Dispatch(ctx, models.NotificationChannel{ChannelType: "webhook", ConfigJSON: string(wCfg)}, ev)
	if err != nil {
		t.Fatalf("Webhook dispatch failed: %v", err)
	}

	// 5. SendTestAlert
	err = d.SendTestAlert(ctx, models.NotificationChannel{ChannelType: "webhook", ConfigJSON: string(wCfg)})
	if err != nil {
		t.Fatalf("SendTestAlert failed: %v", err)
	}

	if !discordReceived || !slackReceived || !ntfyReceived || !webhookReceived {
		t.Errorf("Expected all mock endpoints hit: discord=%v, slack=%v, ntfy=%v, webhook=%v",
			discordReceived, slackReceived, ntfyReceived, webhookReceived)
	}
}
