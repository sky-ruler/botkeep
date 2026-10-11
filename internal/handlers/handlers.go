package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"botkeep/internal/models"
	"botkeep/internal/monitor"
	"botkeep/views/pages"
)

type Handler struct {
	db      *models.DB
	monitor *monitor.Engine
}

func NewHandler(db *models.DB, mon *monitor.Engine) *Handler {
	return &Handler{
		db:      db,
		monitor: mon,
	}
}

// Dashboard renders the primary view with fleet metrics, server table, and live logs.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetAllServers()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load servers: %v", err), http.StatusInternalServerError)
		return
	}

	metrics, err := h.db.GetMetrics()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to calculate metrics: %v", err), http.StatusInternalServerError)
		return
	}

	logs, err := h.db.GetRecentLogs(20)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve telemetry logs: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages.Dashboard(servers, metrics, logs).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// GetServers returns the server table or empty state partial with optional search/filter.
func (h *Handler) GetServers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	region := strings.TrimSpace(r.URL.Query().Get("region"))

	servers, err := h.db.SearchServers(q, status, region)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database query failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ServerTable(servers).Render(r.Context(), w)
}

// GetServerDetails returns the slide-over inspector drawer for a specific server.
func (h *Handler) GetServerDetails(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID provided", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetServerByID(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("Server not found: %v", err), http.StatusNotFound)
		return
	}

	logs, _ := h.db.GetServerLogs(id, 15)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ServerDetailDrawer(*server, logs).Render(r.Context(), w)
}

// DeleteServer removes and decommissions a server instance.
func (h *Handler) DeleteServer(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID provided", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteServer(id); err != nil {
		http.Error(w, fmt.Sprintf("Failed to decommission server: %v", err), http.StatusInternalServerError)
		return
	}

	servers, err := h.db.GetAllServers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerTable(servers).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// StartServer changes server status to active.
func (h *Handler) StartServer(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID provided", http.StatusBadRequest)
		return
	}

	server, err := h.db.UpdateServerStatus(id, "active")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start server %s: %v", id, err), http.StatusNotFound)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerRow(*server).Render(r.Context(), w)
		return
	}

	// Graceful degradation for regular form post
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// StopServer changes server status to stopped.
func (h *Handler) StopServer(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID provided", http.StatusBadRequest)
		return
	}

	server, err := h.db.UpdateServerStatus(id, "stopped")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to stop server %s: %v", id, err), http.StatusNotFound)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerRow(*server).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// RestartServer changes server status to deploying/restarting.
func (h *Handler) RestartServer(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID provided", http.StatusBadRequest)
		return
	}

	server, err := h.db.UpdateServerStatus(id, "restarting")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to restart server %s: %v", id, err), http.StatusNotFound)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerRow(*server).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// CreateServer provisions a new server node with validation.
func (h *Handler) CreateServer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	region := strings.TrimSpace(r.FormValue("region"))
	driver := strings.TrimSpace(r.FormValue("driver"))
	endpointURL := strings.TrimSpace(r.FormValue("endpoint_url"))

	if name == "" {
		http.Error(w, "Validation failed: Node name is required.", http.StatusBadRequest)
		return
	}
	if len(name) < 3 {
		http.Error(w, "Validation failed: Node name must be at least 3 characters.", http.StatusBadRequest)
		return
	}
	if region == "" {
		region = "us-east-1"
	}
	if driver == "" {
		if endpointURL != "" {
			driver = "http"
		} else {
			driver = "demo"
		}
	}

	newSrv, err := h.db.CreateServer(name, region, driver, endpointURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to provision node: %v", err), http.StatusInternalServerError)
		return
	}

	// Trigger immediate health probe if HTTP driver
	if newSrv != nil && newSrv.Driver == "http" && h.monitor != nil {
		go func(id string) {
			_, _ = h.monitor.ProbeNow(context.Background(), id)
		}(newSrv.ID)
	}

	servers, err := h.db.GetAllServers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerTable(servers).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// PingServer triggers an on-demand real probe check on the specified server node.
func (h *Handler) PingServer(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID provided", http.StatusBadRequest)
		return
	}

	var srv *models.Server
	var err error
	if h.monitor != nil {
		srv, err = h.monitor.ProbeNow(r.Context(), id)
	} else {
		srv, err = h.db.GetServerByID(id)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Probe check failed: %v", err), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerRow(*srv).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ClearFleet purges all servers to trigger and verify the empty state component.
func (h *Handler) ClearFleet(w http.ResponseWriter, r *http.Request) {
	if err := h.db.ClearAllServers(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to purge fleet: %v", err), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerTable(nil).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// SimulateError returns a 500 status to demonstrate HTMX error toast notification.
func (h *Handler) SimulateError(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Cluster RPC Exception (500): Control-plane daemon timeout during rolling migration.", http.StatusInternalServerError)
}

// SeedFleet repopulates demo nodes into SQLite.
func (h *Handler) SeedFleet(w http.ResponseWriter, r *http.Request) {
	if err := h.db.SeedDemoFleet(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to seed demo fleet: %v", err), http.StatusInternalServerError)
		return
	}

	servers, err := h.db.GetAllServers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.ServerTable(servers).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GetMetrics returns the cluster metric cards partial for background polling.
func (h *Handler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := h.db.GetMetrics()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch metrics: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.MetricsGrid(metrics).Render(r.Context(), w)
}

// GetLogs returns the live logs terminal entries for periodic streaming.
func (h *Handler) GetLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.db.GetRecentLogs(20)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch logs: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.LogsStream(logs).Render(r.Context(), w)
}

// ClearLogs clears the logs buffer.
func (h *Handler) ClearLogs(w http.ResponseWriter, r *http.Request) {
	_ = h.db.AddLog("system", "INFO", "Telemetry log buffer cleared by administrator.")
	logs, _ := h.db.GetRecentLogs(5)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.LogsStream(logs).Render(r.Context(), w)
}

// PrometheusMetrics exposes standard Prometheus / OpenMetrics scrape format.
func (h *Handler) PrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetAllServers()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve fleet: %v", err), http.StatusInternalServerError)
		return
	}
	metrics, _ := h.db.GetMetrics()

	var sb strings.Builder
	sb.WriteString("# HELP botkeep_servers_total Total count of registered targets in fleet\n")
	sb.WriteString("# TYPE botkeep_servers_total gauge\n")
	sb.WriteString(fmt.Sprintf("botkeep_servers_total %d\n\n", len(servers)))

	if metrics != nil {
		sb.WriteString("# HELP botkeep_servers_active Count of operational targets in fleet\n")
		sb.WriteString("# TYPE botkeep_servers_active gauge\n")
		sb.WriteString(fmt.Sprintf("botkeep_servers_active %d\n\n", metrics.ActiveCount))
	}

	sb.WriteString("# HELP botkeep_node_up Operational status of the monitored target (1 = active/up, 0 = stopped/down)\n")
	sb.WriteString("# TYPE botkeep_node_up gauge\n")
	for _, s := range servers {
		up := 0
		if s.Status == "active" {
			up = 1
		}
		sb.WriteString(fmt.Sprintf("botkeep_node_up{id=\"%s\",name=\"%s\",region=\"%s\",driver=\"%s\",protocol=\"%s\"} %d\n",
			s.ID, s.Name, s.Region, s.Driver, s.Protocol, up))
	}

	sb.WriteString("\n# HELP botkeep_node_latency_ms Sub-millisecond roundtrip probe latency\n")
	sb.WriteString("# TYPE botkeep_node_latency_ms gauge\n")
	for _, s := range servers {
		sb.WriteString(fmt.Sprintf("botkeep_node_latency_ms{id=\"%s\",name=\"%s\"} %.2f\n", s.ID, s.Name, s.LatencyMS))
	}

	sb.WriteString("\n# HELP botkeep_node_uptime_pct Rolling SLA compliance percentage\n")
	sb.WriteString("# TYPE botkeep_node_uptime_pct gauge\n")
	for _, s := range servers {
		sb.WriteString(fmt.Sprintf("botkeep_node_uptime_pct{id=\"%s\",name=\"%s\"} %.2f\n", s.ID, s.Name, s.UptimePct))
	}

	sb.WriteString("\n# HELP botkeep_node_ssl_expiry_days Days until TLS/SSL certificate expiration\n")
	sb.WriteString("# TYPE botkeep_node_ssl_expiry_days gauge\n")
	for _, s := range servers {
		if s.SSLExpiryDays >= 0 {
			sb.WriteString(fmt.Sprintf("botkeep_node_ssl_expiry_days{id=\"%s\",name=\"%s\"} %d\n", s.ID, s.Name, s.SSLExpiryDays))
		}
	}

	if metrics != nil {
		sb.WriteString("\n# HELP botkeep_host_allocated_mb Heap memory allocated by the Go runtime engine in megabytes\n")
		sb.WriteString("# TYPE botkeep_host_allocated_mb gauge\n")
		sb.WriteString(fmt.Sprintf("botkeep_host_allocated_mb %.2f\n", metrics.Host.AllocatedMB))

		sb.WriteString("\n# HELP botkeep_host_goroutines Count of active Go engine goroutines\n")
		sb.WriteString("# TYPE botkeep_host_goroutines gauge\n")
		sb.WriteString(fmt.Sprintf("botkeep_host_goroutines %d\n", metrics.Host.NumGoroutine))

		sb.WriteString("\n# HELP botkeep_host_gc_cycles Total garbage collection passes performed\n")
		sb.WriteString("# TYPE botkeep_host_gc_cycles counter\n")
		sb.WriteString(fmt.Sprintf("botkeep_host_gc_cycles %d\n", metrics.Host.NumGC))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}

// ExportFleet outputs the complete fleet registry as a JSON backup attachment.
func (h *Handler) ExportFleet(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetAllServers()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve fleet: %v", err), http.StatusInternalServerError)
		return
	}

	data, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal fleet backup: %v", err), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("botkeep-fleet-backup-%d.json", time.Now().Unix())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "/", "")
	id = strings.ReplaceAll(id, "\\", "")
	return id
}
