package models

import (
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
	created, err := db.CreateServer("payment-processor-01", "eu-central-1")
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

	created, err := db.CreateServer("ephemeral-canary", "us-east-1")
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

func TestGetHostMetrics(t *testing.T) {
	metrics := GetHostMetrics()
	if metrics.NumGoroutine <= 0 {
		t.Errorf("Expected positive goroutine count, got %d", metrics.NumGoroutine)
	}
	if metrics.GoVersion == "" {
		t.Errorf("Expected Go version string, got empty")
	}
}
