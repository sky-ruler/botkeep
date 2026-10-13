package handlers

import (
	"encoding/json"
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
	r.Get("/status", h.StatusPage)
	r.Get("/status/components", h.StatusComponents)
	r.Get("/api/status", h.StatusJSON)
	r.Post("/admin/incidents/new", h.CreateIncident)
	r.Get("/notifications", h.NotificationsPage)
	r.Get("/notifications/list", h.NotificationChannelsListPartial)
	r.Post("/admin/notifications/new", h.CreateNotificationChannel)
	r.Post("/admin/notifications/{id}/toggle", h.ToggleNotificationChannel)
	r.Delete("/admin/notifications/{id}", h.DeleteNotificationChannel)
	r.Post("/admin/notifications/{id}/test", h.TestNotificationChannel)
	r.Post("/admin/notifications/test-all", h.TestAllNotifications)
	r.Get("/api/notifications", h.NotificationsJSON)
	r.Post("/servers/{id}/synthetic", h.UpdateServerSynthetic)
	r.Post("/servers/{id}/scenario", h.UpdateServerScenario)
	r.Post("/servers/{id}/scenario/test", h.TestServerScenario)
	r.Get("/status/incidents/{id}/postmortem", h.IncidentPostMortem)
	r.Get("/status/incidents/{id}/postmortem/download", h.DownloadIncidentPostMortem)
	r.Post("/api/push/subscribe", h.SubscribeWebPush)
	r.Post("/api/push/unsubscribe", h.UnsubscribeWebPush)
	r.Post("/api/push/test", h.TestWebPush)
	r.Get("/api/push/status", h.GetWebPushStatus)
	r.Delete("/api/push/subscriptions/{id}", h.DeleteWebPushByID)

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

func TestStatusPageHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "BotKeep Status") {
		t.Errorf("Expected body to contain 'BotKeep Status'")
	}
	if !strings.Contains(body, "Past Incidents & Maintenance") {
		t.Errorf("Expected body to contain 'Past Incidents & Maintenance'")
	}
}

func TestStatusJSONHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected application/json, got %s", contentType)
	}
	body := w.Body.String()
	if !strings.Contains(body, "indicator") {
		t.Errorf("Expected JSON to contain indicator field")
	}
	if !strings.Contains(body, "components") {
		t.Errorf("Expected JSON to contain components field")
	}
}

func TestCreateIncidentHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	form := url.Values{}
	form.Set("title", "Upstream DNS Degraded")
	form.Set("status", "investigating")
	form.Set("severity", "minor")
	form.Set("affected_components", "api-gateway")
	form.Set("message", "Investigating slow DNS resolution from upstream provider")

	req := httptest.NewRequest("POST", "/admin/incidents/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 redirect, got %d", w.Code)
	}
}

func TestNotificationsPageHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/notifications", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Alert Channels") {
		t.Errorf("Expected body to contain 'Alert Channels'")
	}
}

func TestCreateAndToggleNotificationChannelHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Create Discord channel
	form := url.Values{}
	form.Set("name", "Production Discord Alerts")
	form.Set("channel_type", "discord")
	form.Set("webhook_url", "https://discord.com/api/webhooks/test/12345")
	form.Set("events_down", "on")
	form.Set("events_recovery", "on")

	req := httptest.NewRequest("POST", "/admin/notifications/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 on create, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Production Discord Alerts") {
		t.Errorf("Expected channels list to contain 'Production Discord Alerts'")
	}

	// 2. Query JSON API
	reqAPI := httptest.NewRequest("GET", "/api/notifications", nil)
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)

	if wAPI.Code != http.StatusOK {
		t.Errorf("Expected status 200 on /api/notifications, got %d", wAPI.Code)
	}
	apiBody := wAPI.Body.String()
	if !strings.Contains(apiBody, "Production Discord Alerts") {
		t.Errorf("Expected API JSON to include created channel")
	}

	// 3. Toggle channel
	reqToggle := httptest.NewRequest("POST", "/admin/notifications/nc-discord-01/toggle", nil)
	wToggle := httptest.NewRecorder()
	r.ServeHTTP(wToggle, reqToggle)

	if wToggle.Code != http.StatusOK {
		t.Errorf("Expected status 200 on toggle, got %d", wToggle.Code)
	}

	// 4. Delete channel
	reqDel := httptest.NewRequest("DELETE", "/admin/notifications/nc-discord-01", nil)
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusOK {
		t.Errorf("Expected status 200 on delete, got %d", wDel.Code)
	}
}

func TestTestNotificationChannelHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// Testing with a demo channel
	req := httptest.NewRequest("POST", "/admin/notifications/nc-ntfy-01/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Handler returns an HTMX toast
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 with toast, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "role=\"alert\"") {
		t.Errorf("Expected toast component returned")
	}
}

func TestUpdateServerSyntheticHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Create a server first
	formCreate := url.Values{
		"name":         {"synthetic-api"},
		"region":       {"us-east-1"},
		"driver":       {"http"},
		"endpoint_url": {"https://api.example.com/healthz"},
	}
	reqCreate := httptest.NewRequest("POST", "/servers/new", strings.NewReader(formCreate.Encode()))
	reqCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqCreate.Header.Set("HX-Request", "true")
	wCreate := httptest.NewRecorder()
	r.ServeHTTP(wCreate, reqCreate)

	if wCreate.Code != http.StatusOK {
		t.Fatalf("Failed to create server: status %d", wCreate.Code)
	}

	// 2. Update synthetic rules for a demo node
	form := url.Values{
		"http_method":     {"POST"},
		"expected_status": {"201"},
		"body_assertion":  {"created"},
		"custom_headers":  {"X-App-Env: prod\nAuthorization: Bearer secret"},
	}
	req := httptest.NewRequest("POST", "/servers/srv-us-101/synthetic", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 on synthetic update, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "detail-drawer") {
		t.Errorf("Expected detail-drawer returned in response")
	}
}

func TestUpdateServerScenarioHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	scenarioPayload := `[{"name":"Step 1","method":"GET","url":"https://example.com","expected_status":200}]`
	form := url.Values{
		"scenario_steps": {scenarioPayload},
	}
	req := httptest.NewRequest("POST", "/servers/srv-us-101/scenario", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 on scenario update, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "detail-drawer") {
		t.Errorf("Expected detail-drawer returned in response")
	}
}

func TestTestServerScenarioHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Initially empty scenario -> should return warning toast
	req := httptest.NewRequest("POST", "/servers/srv-us-101/scenario/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 with toast, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "role=\"alert\"") {
		t.Errorf("Expected toast component returned")
	}
}

func TestIncidentPostMortemHandler(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Create a test incident via admin route
	formInc := url.Values{
		"title":               {"Network Transit Packet Drop"},
		"status":              {"resolved"},
		"severity":            {"major"},
		"affected_components": {"cloudflare-cdn-edge"},
		"message":             {"Upstream tier-1 transit link flap."},
	}
	reqCreate := httptest.NewRequest("POST", "/admin/incidents/new", strings.NewReader(formInc.Encode()))
	reqCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wCreate := httptest.NewRecorder()
	r.ServeHTTP(wCreate, reqCreate)

	// 2. Fetch seeded incident from NewDB (e.g., inc-2026-001)
	reqView := httptest.NewRequest("GET", "/status/incidents/inc-2026-001/postmortem", nil)
	wView := httptest.NewRecorder()
	r.ServeHTTP(wView, reqView)

	if wView.Code != http.StatusOK {
		t.Errorf("Expected status 200 on post-mortem view, got %d", wView.Code)
	}
	if !strings.Contains(wView.Body.String(), "Incident Post-Mortem") {
		t.Errorf("Expected Post-Mortem page returned")
	}

	// 3. Test markdown download endpoint
	reqDown := httptest.NewRequest("GET", "/status/incidents/inc-2026-001/postmortem/download", nil)
	wDown := httptest.NewRecorder()
	r.ServeHTTP(wDown, reqDown)

	if wDown.Code != http.StatusOK {
		t.Errorf("Expected status 200 on post-mortem download, got %d", wDown.Code)
	}
	if !strings.Contains(wDown.Body.String(), "# Incident Post-Mortem:") {
		t.Errorf("Expected Markdown content in download output")
	}
	if wDown.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Errorf("Expected markdown Content-Type, got %s", wDown.Header().Get("Content-Type"))
	}
}

func TestWebPushHandlers(t *testing.T) {
	r, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Subscribe Web Push
	subJSON := `{"endpoint":"https://fcm.googleapis.com/fcm/send/test-device-token","p256dh":"test-key","auth":"test-auth","user_agent":"TestBrowser/1.0"}`
	reqSub := httptest.NewRequest("POST", "/api/push/subscribe", strings.NewReader(subJSON))
	reqSub.Header.Set("Content-Type", "application/json")
	wSub := httptest.NewRecorder()
	r.ServeHTTP(wSub, reqSub)

	if wSub.Code != http.StatusCreated {
		t.Fatalf("Expected status 201 on subscribe, got %d: %s", wSub.Code, wSub.Body.String())
	}

	var subResp map[string]interface{}
	if err := json.Unmarshal(wSub.Body.Bytes(), &subResp); err != nil {
		t.Fatalf("Failed to parse subscribe response: %v", err)
	}
	subID, ok := subResp["id"].(string)
	if !ok || subID == "" {
		t.Fatalf("Expected valid subscription ID in response")
	}

	// 2. Check Push Status
	reqStatus := httptest.NewRequest("GET", "/api/push/status", nil)
	wStatus := httptest.NewRecorder()
	r.ServeHTTP(wStatus, reqStatus)

	if wStatus.Code != http.StatusOK {
		t.Errorf("Expected status 200 on push status, got %d", wStatus.Code)
	}
	var statusResp map[string]interface{}
	_ = json.Unmarshal(wStatus.Body.Bytes(), &statusResp)
	if int(statusResp["count"].(float64)) != 1 {
		t.Errorf("Expected 1 active subscription, got %v", statusResp["count"])
	}

	// 3. Test Push Broadcast
	reqTest := httptest.NewRequest("POST", "/api/push/test", nil)
	wTest := httptest.NewRecorder()
	r.ServeHTTP(wTest, reqTest)

	if wTest.Code != http.StatusOK {
		t.Errorf("Expected status 200 on push test, got %d", wTest.Code)
	}

	// 4. Delete Subscription by ID
	reqDel := httptest.NewRequest("DELETE", "/api/push/subscriptions/"+subID, nil)
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusOK {
		t.Errorf("Expected status 200 on delete subscription, got %d", wDel.Code)
	}

	// 5. Verify count is now 0
	wStatusAfter := httptest.NewRecorder()
	r.ServeHTTP(wStatusAfter, reqStatus)
	var statusAfter map[string]interface{}
	_ = json.Unmarshal(wStatusAfter.Body.Bytes(), &statusAfter)
	if int(statusAfter["count"].(float64)) != 0 {
		t.Errorf("Expected 0 active subscriptions after deletion, got %v", statusAfter["count"])
	}
}



