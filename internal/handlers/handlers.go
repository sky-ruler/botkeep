package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"botkeep/internal/models"
	"botkeep/views/pages"
)

type Handler struct {
	db *models.DB
}

func NewHandler(db *models.DB) *Handler {
	return &Handler{db: db}
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

	_, err := h.db.CreateServer(name, region)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to provision node: %v", err), http.StatusInternalServerError)
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

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "/", "")
	id = strings.ReplaceAll(id, "\\", "")
	return id
}
