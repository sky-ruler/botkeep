package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestDBForPostMortem(t *testing.T) (*DB, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "botkeep-postmortem-test-*")
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

func TestGeneratePostMortem(t *testing.T) {
	db, cleanup := setupTestDBForPostMortem(t)
	defer cleanup()

	// 1. Create a resolved test incident
	inc, err := db.CreateIncident(
		"Redis Replication Desynchronization",
		"resolved",
		"major",
		"redis-cache-fra, postgres-cluster-pri",
		"Replication buffer overflow on Frankfurt secondary replica due to burst write IOPS.",
	)
	if err != nil {
		t.Fatalf("Failed to create incident: %v", err)
	}

	// 2. Generate Post-Mortem
	pm, err := db.GeneratePostMortem(inc.ID)
	if err != nil {
		t.Fatalf("Failed to generate post-mortem: %v", err)
	}

	if pm.IncidentID != inc.ID {
		t.Errorf("Expected incident ID %s, got %s", inc.ID, pm.IncidentID)
	}
	if pm.Severity != "major" {
		t.Errorf("Expected severity major, got %s", pm.Severity)
	}
	if len(pm.TimelineEvents) == 0 {
		t.Errorf("Expected timeline events generated, got 0")
	}
	if len(pm.ActionItems) == 0 {
		t.Errorf("Expected preventative action items, got 0")
	}

	// 3. Test Markdown export
	md := pm.ToMarkdown()
	if !strings.Contains(md, "# Incident Post-Mortem: Redis Replication Desynchronization") {
		t.Errorf("Expected title in markdown export")
	}
	if !strings.Contains(md, "## 1. Executive Summary") {
		t.Errorf("Expected Executive Summary section in markdown")
	}
	if !strings.Contains(md, "## 4. Chronological Incident Timeline") {
		t.Errorf("Expected timeline section in markdown")
	}
	if !strings.Contains(md, "## 5. Preventative Action Items") {
		t.Errorf("Expected action items in markdown")
	}

	// 4. Test JSON export
	jsonData, err := pm.ToJSON()
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}
	if !strings.Contains(string(jsonData), "incident_id") {
		t.Errorf("Expected valid JSON output with incident_id")
	}
}

func TestGeneratePostMortemNotFound(t *testing.T) {
	db, cleanup := setupTestDBForPostMortem(t)
	defer cleanup()

	_, err := db.GeneratePostMortem("non-existent-inc-999")
	if err == nil {
		t.Errorf("Expected error for non-existent incident, got nil")
	}
}
