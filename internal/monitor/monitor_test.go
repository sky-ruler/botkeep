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

func TestCheckStatusMatch(t *testing.T) {
	tests := []struct {
		code     int
		expected string
		match    bool
	}{
		{200, "200", true},
		{200, "", true},
		{201, "200", false},
		{201, "200,201", true},
		{204, "2xx", true},
		{301, "2xx", false},
		{302, "3xx", true},
		{404, "200-299", false},
		{202, "200-299", true},
		{503, "500,502,503,504", true},
		{400, "5xx", false},
	}

	for _, tt := range tests {
		got := CheckStatusMatch(tt.code, tt.expected)
		if got != tt.match {
			t.Errorf("CheckStatusMatch(%d, %q) = %v, want %v", tt.code, tt.expected, got, tt.match)
		}
	}
}

func TestProbeServerSynthetic(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Mock server verifying custom header and body response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Secret") != "ultra-pass" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"all_systems_green","version":"2.0"}`))
	}))
	defer ts.Close()

	srv, err := db.CreateServer("synthetic-target", "us-east-1", "http", ts.URL)
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	// 1. Configure synthetic rules matching the secret and keyword
	err = db.UpdateServerSynthetic(srv.ID, "GET", "200", "all_systems_green", "X-Custom-Secret: ultra-pass")
	if err != nil {
		t.Fatalf("UpdateServerSynthetic failed: %v", err)
	}

	updatedSrv, _ := db.GetServerByID(srv.ID)
	engine := NewEngine(db, time.Minute)
	err = engine.ProbeServer(context.Background(), *updatedSrv)
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}

	checked, _ := db.GetServerByID(srv.ID)
	if checked.Status != "active" {
		t.Errorf("Expected server active with valid synthetic assertions, got %s", checked.Status)
	}

	// 2. Configure body assertion that is missing -> should fail probe
	_ = db.UpdateServerSynthetic(srv.ID, "GET", "200", "non_existent_keyword", "X-Custom-Secret: ultra-pass")
	updatedSrv2, _ := db.GetServerByID(srv.ID)
	_ = engine.ProbeServer(context.Background(), *updatedSrv2)

	checked2, _ := db.GetServerByID(srv.ID)
	if checked2.Status != "stopped" {
		t.Errorf("Expected server stopped when keyword assertion fails, got %s", checked2.Status)
	}
}

