package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "botkeep-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
	}

	return db, cleanup
}

func TestDBInitializationAndSeed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	count, err := db.CountServers()
	if err != nil {
		t.Fatalf("CountServers failed: %v", err)
	}

	// Should be auto-seeded with demo fleet
	if count == 0 {
		t.Errorf("Expected auto-seeded servers, got 0")
	}

	servers, err := db.GetAllServers()
	if err != nil {
		t.Fatalf("GetAllServers failed: %v", err)
	}
	if len(servers) != count {
		t.Errorf("Expected %d servers, got %d", count, len(servers))
	}
}

func TestCreateAndSearchServer(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Provision new node
	created, err := db.CreateServer("payment-processor-01", "eu-central-1", "demo", "")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if created.Name != "payment-processor-01" {
		t.Errorf("Expected name payment-processor-01, got %s", created.Name)
	}
	if created.Region != "eu-central-1" {
		t.Errorf("Expected region eu-central-1, got %s", created.Region)
	}

	// Search by name
	results, err := db.SearchServers("payment-processor", "all", "all")
	if err != nil {
		t.Fatalf("SearchServers failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 search result, got %d", len(results))
	}

	// Filter by region
	euResults, err := db.SearchServers("", "all", "eu-central-1")
	if err != nil {
		t.Fatalf("SearchServers with region filter failed: %v", err)
	}
	for _, s := range euResults {
		if s.Region != "eu-central-1" {
			t.Errorf("Expected eu-central-1 region, got %s", s.Region)
		}
	}
}

func TestServerStateTransitions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	servers, err := db.GetAllServers()
	if err != nil || len(servers) == 0 {
		t.Fatalf("Failed to fetch seed servers: %v", err)
	}

	target := servers[0]

	// Stop server
	stopped, err := db.UpdateServerStatus(target.ID, "stopped")
	if err != nil {
		t.Fatalf("UpdateServerStatus to stopped failed: %v", err)
	}
	if stopped.Status != "stopped" {
		t.Errorf("Expected status stopped, got %s", stopped.Status)
	}
	if stopped.CPUUsage != 0.0 {
		t.Errorf("Expected 0.0 CPU on stopped server, got %f", stopped.CPUUsage)
	}

	// Start server
	started, err := db.UpdateServerStatus(target.ID, "active")
	if err != nil {
		t.Fatalf("UpdateServerStatus to active failed: %v", err)
	}
	if started.Status != "active" {
		t.Errorf("Expected status active, got %s", started.Status)
	}
}

func TestDeleteServer(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	created, err := db.CreateServer("ephemeral-canary", "us-east-1", "http", "https://example.com/health")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	err = db.DeleteServer(created.ID)
	if err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	// Verify not found
	_, err = db.GetServerByID(created.ID)
	if err == nil {
		t.Errorf("Expected error fetching deleted server, got nil")
	}
}

func TestRecordProbe(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	s, err := db.CreateServer("api-gateway", "us-east-1", "http", "https://example.com")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	err = db.RecordProbe(s.ID, 18.5, 200, true, "")
	if err != nil {
		t.Fatalf("RecordProbe failed: %v", err)
	}

	updated, err := db.GetServerByID(s.ID)
	if err != nil {
		t.Fatalf("GetServerByID failed: %v", err)
	}
	if updated.LatencyMS != 18.5 {
		t.Errorf("Expected LatencyMS 18.5, got %f", updated.LatencyMS)
	}
	if updated.UptimePct != 100.0 {
		t.Errorf("Expected UptimePct 100.0, got %f", updated.UptimePct)
	}
	if len(updated.ProbePoints) == 0 {
		t.Errorf("Expected probe history points, got 0")
	}
}

func TestGetHostMetrics(t *testing.T) {
	metrics := GetHostMetrics()
	if metrics.NumGoroutine <= 0 {
		t.Errorf("Expected positive goroutine count, got %d", metrics.NumGoroutine)
	}
	if metrics.GoVersion == "" {
		t.Errorf("Expected Go version string, got empty")
	}
}

func TestPublicServersAndIncidents(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Verify auto-seeded public servers
	pubServers, err := db.GetPublicServers()
	if err != nil {
		t.Fatalf("GetPublicServers failed: %v", err)
	}
	if len(pubServers) == 0 {
		t.Errorf("Expected public servers seeded, got 0")
	}

	// 2. Verify auto-seeded incidents
	incidents, err := db.GetRecentIncidents(10)
	if err != nil {
		t.Fatalf("GetRecentIncidents failed: %v", err)
	}
	if len(incidents) == 0 {
		t.Errorf("Expected demo incidents seeded, got 0")
	}

	// 3. Create a new incident and resolve it
	newInc, err := db.CreateIncident("High Memory Usage on Edge Node", "investigating", "minor", "srv-us-101", "Investigating heap usage spike")
	if err != nil {
		t.Fatalf("CreateIncident failed: %v", err)
	}
	if newInc.Status != "investigating" {
		t.Errorf("Expected status investigating, got %s", newInc.Status)
	}

	err = db.ResolveIncident(newInc.ID)
	if err != nil {
		t.Fatalf("ResolveIncident failed: %v", err)
	}

	// 4. Toggle visibility
	err = db.ToggleServerVisibility(pubServers[0].ID, false)
	if err != nil {
		t.Fatalf("ToggleServerVisibility failed: %v", err)
	}
	updatedPub, err := db.GetPublicServers()
	if err != nil {
		t.Fatalf("GetPublicServers after toggle failed: %v", err)
	}
	if len(updatedPub) != len(pubServers)-1 {
		t.Errorf("Expected %d public servers after toggle, got %d", len(pubServers)-1, len(updatedPub))
	}
}

func TestNotificationChannels(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Verify bootstrapped demo channels
	channels, err := db.GetNotificationChannels()
	if err != nil {
		t.Fatalf("GetNotificationChannels failed: %v", err)
	}
	if len(channels) == 0 {
		t.Errorf("Expected bootstrapped notification channels, got 0")
	}

	// 2. Create new custom channel
	cfgJSON, _ := json.Marshal(DiscordConfig{WebhookURL: "https://discord.com/api/webhooks/test/123"})
	newCh := NotificationChannel{
		ID:          "nc-custom-discord",
		Name:        "Custom Discord Channel",
		ChannelType: "discord",
		ConfigJSON:  string(cfgJSON),
		Enabled:     true,
		Events:      "down,recovery",
	}
	err = db.CreateNotificationChannel(newCh)
	if err != nil {
		t.Fatalf("CreateNotificationChannel failed: %v", err)
	}

	// 3. Fetch by ID
	fetched, err := db.GetNotificationChannelByID("nc-custom-discord")
	if err != nil {
		t.Fatalf("GetNotificationChannelByID failed: %v", err)
	}
	if fetched.Name != "Custom Discord Channel" {
		t.Errorf("Expected name 'Custom Discord Channel', got '%s'", fetched.Name)
	}

	// 4. Toggle enabled
	newState, err := db.ToggleNotificationChannel("nc-custom-discord")
	if err != nil {
		t.Fatalf("ToggleNotificationChannel failed: %v", err)
	}
	if newState != false {
		t.Errorf("Expected newState false, got %v", newState)
	}

	// 5. Update last dispatched
	err = db.UpdateNotificationChannelLastDispatched("nc-custom-discord")
	if err != nil {
		t.Fatalf("UpdateNotificationChannelLastDispatched failed: %v", err)
	}
	updated, err := db.GetNotificationChannelByID("nc-custom-discord")
	if err != nil || updated.LastDispatchedAt == nil {
		t.Errorf("Expected non-nil LastDispatchedAt after update, got %v", updated.LastDispatchedAt)
	}

	// 6. Delete channel
	err = db.DeleteNotificationChannel("nc-custom-discord")
	if err != nil {
		t.Fatalf("DeleteNotificationChannel failed: %v", err)
	}
	_, err = db.GetNotificationChannelByID("nc-custom-discord")
	if err == nil {
		t.Errorf("Expected error fetching deleted channel, got nil")
	}
}

func TestSatelliteMeshAndConsensus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Verify seed satellites
	sats, err := db.GetSatellites()
	if err != nil {
		t.Fatalf("GetSatellites failed: %v", err)
	}
	if len(sats) < 3 {
		t.Errorf("Expected at least 3 seed satellites, got %d", len(sats))
	}

	// 2. Register new satellite
	customSat := Satellite{
		ID:             "sat-ap-syd-01",
		Name:           "Sydney Edge Satellite",
		Region:         "ap-southeast-2",
		IPAddress:      "198.51.100.200",
		Status:         "online",
		LatencyToHubMS: 165.2,
		Version:        "2.0.0",
	}
	err = db.RegisterOrUpdateSatellite(customSat)
	if err != nil {
		t.Fatalf("RegisterOrUpdateSatellite failed: %v", err)
	}

	// Verify it shows up
	sats, err = db.GetSatellites()
	if err != nil {
		t.Fatalf("GetSatellites failed: %v", err)
	}
	found := false
	for _, s := range sats {
		if s.ID == "sat-ap-syd-01" {
			found = true
			if s.Region != "ap-southeast-2" {
				t.Errorf("Expected region ap-southeast-2, got %s", s.Region)
			}
		}
	}
	if !found {
		t.Errorf("Newly registered satellite sat-ap-syd-01 not found")
	}

	// 3. Record Satellite Probes for a test server
	servers, err := db.GetAllServers()
	if err != nil || len(servers) == 0 {
		t.Fatalf("No servers found: %v", err)
	}
	targetID := servers[0].ID

	// Multi-region probes: US passes, EU fails, AP fails -> Consensus down!
	_ = db.RecordSatelliteProbe(SatelliteProbe{
		SatelliteID: "sat-us-east-01",
		ServerID:    targetID,
		Region:      "us-east-1",
		LatencyMS:   22.4,
		StatusCode:  200,
		Success:     true,
	})
	_ = db.RecordSatelliteProbe(SatelliteProbe{
		SatelliteID:  "sat-eu-fra-01",
		ServerID:     targetID,
		Region:       "eu-central-1",
		LatencyMS:    0,
		StatusCode:   503,
		Success:      false,
		ErrorMessage: "i/o timeout across Frankfurt transit",
	})
	_ = db.RecordSatelliteProbe(SatelliteProbe{
		SatelliteID:  "sat-ap-tokyo-01",
		ServerID:     targetID,
		Region:       "ap-northeast-1",
		LatencyMS:    0,
		StatusCode:   503,
		Success:      false,
		ErrorMessage: "connection refused",
	})

	// 4. Test EvaluateConsensus
	isDown, failingRegions, total, err := db.EvaluateConsensus(targetID)
	if err != nil {
		t.Fatalf("EvaluateConsensus failed: %v", err)
	}
	if !isDown {
		t.Errorf("Expected consensus down (2 regions failed), got false")
	}
	if len(failingRegions) != 2 {
		t.Errorf("Expected 2 failing regions, got %d (%v)", len(failingRegions), failingRegions)
	}
	if total < 3 {
		t.Errorf("Expected at least 3 total reporting regions, got %d", total)
	}

	// 5. Verify server regional summaries
	srv, err := db.GetServerByID(targetID)
	if err != nil {
		t.Fatalf("GetServerByID failed: %v", err)
	}
	if len(srv.RegionalProbes) == 0 {
		t.Errorf("Expected regional probe summaries on Server, got 0")
	}
}

