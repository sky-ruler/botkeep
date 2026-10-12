package gitops

import (
	"os"
	"path/filepath"
	"testing"

	"botkeep/internal/models"
)

func setupTestDB(t *testing.T) (*models.DB, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "botkeep-gitops-test-*")
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

func TestGitOpsExportAndReconcile(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Export fleet to YAML
	yamlBytes, err := ExportFleetConfig(db)
	if err != nil {
		t.Fatalf("ExportFleetConfig failed: %v", err)
	}
	if len(yamlBytes) == 0 {
		t.Fatalf("Expected non-empty YAML export")
	}

	// 2. Write custom fleet.yaml and test LoadFleetConfig
	tempDir, _ := os.MkdirTemp("", "botkeep-yaml-*")
	defer os.RemoveAll(tempDir)

	fleetPath := filepath.Join(tempDir, "fleet.yaml")
	customYAML := `
version: "2.0"
targets:
  - name: "gitops-api-service"
    driver: "http"
    url: "https://api.example.com"
    region: "us-east-1"
    runbook_url: "https://api.github.com/repos/org/repo/dispatches"
    runbook_enabled: true
  - name: "gitops-cache-cluster"
    driver: "tcp"
    url: "cache.internal:6379"
    region: "eu-central-1"
`
	if err := os.WriteFile(fleetPath, []byte(customYAML), 0644); err != nil {
		t.Fatalf("Failed to write fleet.yaml: %v", err)
	}

	cfg, err := LoadFleetConfig(fleetPath)
	if err != nil {
		t.Fatalf("LoadFleetConfig failed: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("Expected 2 targets in YAML, got %d", len(cfg.Targets))
	}

	// 3. Reconcile with DB
	created, _, err := ReconcileFleet(db, cfg)
	if err != nil {
		t.Fatalf("ReconcileFleet failed: %v", err)
	}
	if created != 2 {
		t.Errorf("Expected 2 created targets, got %d", created)
	}

	// Reconcile again: should update, not re-create
	created2, updated2, err := ReconcileFleet(db, cfg)
	if err != nil {
		t.Fatalf("Second ReconcileFleet failed: %v", err)
	}
	if created2 != 0 || updated2 != 2 {
		t.Errorf("Expected 0 created and 2 updated on re-sync, got %d created, %d updated", created2, updated2)
	}

	// Verify server in DB has runbook properties
	all, _ := db.GetAllServers()
	found := false
	for _, s := range all {
		if s.Name == "gitops-api-service" {
			found = true
			if !s.RunbookEnabled || s.RunbookURL != "https://api.github.com/repos/org/repo/dispatches" {
				t.Errorf("Runbook settings not synced properly: %+v", s)
			}
		}
	}
	if !found {
		t.Errorf("Expected gitops-api-service in DB after reconciliation")
	}
}
