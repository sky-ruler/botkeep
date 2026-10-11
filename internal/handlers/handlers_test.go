package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"botkeep/internal/models"
	"botkeep/internal/monitor"
)

func setupTestServer(t *testing.T) (*chi.Mux, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "botkeep-handler-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := models.NewDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize test DB: %v", err)
	}

	mon := monitor.NewEngine(db, time.Minute)
	h := NewHandler(db, mon)
	r := chi.NewRouter()

	r.Get("/", h.Dashboard)
	r.Get("/servers", h.GetServers)
	r.Post("/servers/new", h.CreateServer)
	r.Post("/servers/clear", h.ClearFleet)
	r.Post("/servers/simulate-error", h.SimulateError)
	r.Get("/servers/{id}/details", h.GetServerDetails)
	r.Delete("/servers/{id}", h.DeleteServer)
	r.Post("/servers/{id}/start", h.StartServer)
	r.Post("/servers/{id}/stop", h.StopServer)
	r.Post("/servers/{id}/restart", h.RestartServer)
	r.Post("/servers/{id}/ping", h.PingServer)
	r.Post("/servers/seed", h.SeedFleet)
	r.Get("/metrics", h.GetMetrics)
	r.Get("/metrics/prometheus", h.PrometheusMetrics)
	r.Get("/api/fleet/export", h.ExportFleet)
	r.Get("/logs", h.GetLogs)

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
	}

	return r, cleanup
}

func TestDashboardHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "BotKeep") {
		t.Errorf("Expected body to contain 'BotKeep'")
	}
}

func TestServerActionsHTMX(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// Stop action
	req := httptest.NewRequest("POST", "/servers/srv-us-101/stop", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "server-row-srv-us-101") {
		t.Errorf("Expected response to contain updated server row")
	}

	// Start action
	req = httptest.NewRequest("POST", "/servers/srv-us-101/start", nil)
	req.Header.Set("HX-Request", "true")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestCreateServerValidation(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// Empty name validation failure
	form := url.Values{}
	form.Set("name", "")
	req := httptest.NewRequest("POST", "/servers/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for empty name, got %d", w.Code)
	}

	// Valid creation
	form.Set("name", "analytics-worker-01")
	form.Set("region", "eu-central-1")
	req = httptest.NewRequest("POST", "/servers/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "analytics-worker-01") {
		t.Errorf("Expected table to contain newly created server")
	}
}

func TestSimulateErrorToast(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/servers/simulate-error", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestPingServerHTMX(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/servers/srv-us-101/ping", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "server-row-srv-us-101") {
		t.Errorf("Expected response to contain updated server row")
	}
}

func TestPrometheusMetrics(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/metrics/prometheus", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "botkeep_servers_total") {
		t.Errorf("Expected body to contain 'botkeep_servers_total'")
	}
	if !strings.Contains(body, "botkeep_host_allocated_mb") {
		t.Errorf("Expected body to contain 'botkeep_host_allocated_mb'")
	}
}

func TestExportFleet(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/fleet/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected JSON content type, got %s", contentType)
	}
	if !strings.Contains(w.Body.String(), "srv-us-101") {
		t.Errorf("Expected JSON export to contain seed server srv-us-101")
	}
}
