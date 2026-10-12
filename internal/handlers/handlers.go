package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"botkeep/internal/gitops"
	"botkeep/internal/models"
	"botkeep/internal/monitor"
	"botkeep/views/components"
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
	remediations, _ := h.db.GetServerRemediations(id, 5)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ServerDetailDrawer(*server, logs, remediations).Render(r.Context(), w)
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

// StatusPage renders the public-facing status page.
func (h *Handler) StatusPage(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetPublicServers()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load status components: %v", err), http.StatusInternalServerError)
		return
	}

	incidents, _ := h.db.GetRecentIncidents(10)

	totalSLA := 0.0
	totalLatency := 0.0
	activeCount := 0
	for _, s := range servers {
		totalSLA += s.UptimePct
		if s.Status == "active" {
			totalLatency += s.LatencyMS
			activeCount++
		}
	}

	overallSLA := 100.0
	if len(servers) > 0 {
		overallSLA = totalSLA / float64(len(servers))
	}
	avgLatency := 0.0
	if activeCount > 0 {
		avgLatency = totalLatency / float64(activeCount)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.PublicStatusPage(servers, incidents, overallSLA, avgLatency, time.Now().UTC()).Render(r.Context(), w)
}

// StatusComponents renders just the component list for public HTMX polling.
func (h *Handler) StatusComponents(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetPublicServers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.StatusComponentsList(servers).Render(r.Context(), w)
}

// StatusJSON returns a programmatic JSON summary of the system status.
func (h *Handler) StatusJSON(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetPublicServers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	incidents, _ := h.db.GetRecentIncidents(5)

	stopped := 0
	for _, s := range servers {
		if s.Status == "stopped" {
			stopped++
		}
	}

	statusIndicator := "operational"
	description := "All systems operational"
	if stopped == 1 {
		statusIndicator = "degraded"
		description = "Partial system degradation"
	} else if stopped > 1 {
		statusIndicator = "outage"
		description = "Major system outage detected"
	}

	payload := map[string]any{
		"status": map[string]any{
			"indicator":   statusIndicator,
			"description": description,
		},
		"components": servers,
		"incidents":  incidents,
		"updated_at": time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(payload)
}

// CreateIncident handles administrative incident creation.
func (h *Handler) CreateIncident(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	status := strings.TrimSpace(r.FormValue("status"))
	severity := strings.TrimSpace(r.FormValue("severity"))
	affected := strings.TrimSpace(r.FormValue("affected_components"))
	message := strings.TrimSpace(r.FormValue("message"))

	if title == "" || message == "" {
		http.Error(w, "Title and message are required", http.StatusBadRequest)
		return
	}
	if status == "" {
		status = "investigating"
	}
	if severity == "" {
		severity = "minor"
	}

	_, err := h.db.CreateIncident(title, status, severity, affected, message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/status", http.StatusSeeOther)
}

// NotificationsPage renders the notification channel management page.
func (h *Handler) NotificationsPage(w http.ResponseWriter, r *http.Request) {
	channels, err := h.db.GetNotificationChannels()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load notification channels: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.NotificationsPage(channels).Render(r.Context(), w)
}

// NotificationChannelsListPartial returns the rendered channel list for HTMX swaps.
func (h *Handler) NotificationChannelsListPartial(w http.ResponseWriter, r *http.Request) {
	channels, err := h.db.GetNotificationChannels()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ChannelsList(channels).Render(r.Context(), w)
}

// CreateNotificationChannel handles registration of a new alert channel.
func (h *Handler) CreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	channelType := strings.TrimSpace(r.FormValue("channel_type"))
	if name == "" || channelType == "" {
		http.Error(w, "Channel name and type are required", http.StatusBadRequest)
		return
	}

	var cfgJSON []byte
	switch strings.ToLower(channelType) {
	case "discord":
		webhookURL := strings.TrimSpace(r.FormValue("webhook_url"))
		cfgJSON, _ = json.Marshal(models.DiscordConfig{WebhookURL: webhookURL})
	case "slack":
		webhookURL := strings.TrimSpace(r.FormValue("webhook_url"))
		cfgJSON, _ = json.Marshal(models.SlackConfig{WebhookURL: webhookURL})
	case "telegram":
		token := strings.TrimSpace(r.FormValue("telegram_token"))
		chatID := strings.TrimSpace(r.FormValue("telegram_chat_id"))
		cfgJSON, _ = json.Marshal(models.TelegramConfig{BotToken: token, ChatID: chatID})
	case "ntfy":
		topic := strings.TrimSpace(r.FormValue("ntfy_topic"))
		server := strings.TrimSpace(r.FormValue("ntfy_server"))
		if server == "" {
			server = "https://ntfy.sh"
		}
		cfgJSON, _ = json.Marshal(models.NtfyConfig{Server: server, Topic: topic})
	default:
		channelType = "webhook"
		webhookURL := strings.TrimSpace(r.FormValue("webhook_url"))
		cfgJSON, _ = json.Marshal(models.WebhookConfig{URL: webhookURL})
	}

	var events []string
	if r.FormValue("events_down") != "" {
		events = append(events, "down")
	}
	if r.FormValue("events_recovery") != "" {
		events = append(events, "recovery")
	}
	eventStr := strings.Join(events, ",")
	if eventStr == "" {
		eventStr = "down,recovery"
	}

	nc := models.NotificationChannel{
		Name:        name,
		ChannelType: channelType,
		ConfigJSON:  string(cfgJSON),
		Enabled:     true,
		Events:      eventStr,
	}

	if err := h.db.CreateNotificationChannel(nc); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create channel: %v", err), http.StatusInternalServerError)
		return
	}

	channels, err := h.db.GetNotificationChannels()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ChannelsList(channels).Render(r.Context(), w)
}

// ToggleNotificationChannel toggles the enabled state of an alert channel.
func (h *Handler) ToggleNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Channel ID required", http.StatusBadRequest)
		return
	}

	_, err := h.db.ToggleNotificationChannel(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	channels, err := h.db.GetNotificationChannels()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ChannelsList(channels).Render(r.Context(), w)
}

// DeleteNotificationChannel removes a notification channel.
func (h *Handler) DeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Channel ID required", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteNotificationChannel(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	channels, err := h.db.GetNotificationChannels()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ChannelsList(channels).Render(r.Context(), w)
}

// TestNotificationChannel sends an immediate verification ping to a specific channel and returns an HTMX toast.
func (h *Handler) TestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Channel ID required", http.StatusBadRequest)
		return
	}

	ch, err := h.db.GetNotificationChannelByID(id)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = components.Toast("Channel not found", "error").Render(r.Context(), w)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.monitor.GetDispatcher().SendTestAlert(ctx, *ch); err != nil {
		_ = h.db.AddLog(id, "WARN", fmt.Sprintf("[Test] Failed delivery to %s (%s): %v", ch.Name, ch.ChannelType, err))
		_ = components.Toast(fmt.Sprintf("Delivery failed to %s: %v", ch.Name, err), "error").Render(r.Context(), w)
		return
	}

	_ = h.db.UpdateNotificationChannelLastDispatched(ch.ID)
	_ = h.db.AddLog(id, "INFO", fmt.Sprintf("[Test] Delivered test alert to %s (%s)", ch.Name, ch.ChannelType))
	_ = components.Toast(fmt.Sprintf("Test alert delivered successfully to %s (%s)", ch.Name, ch.ChannelType), "success").Render(r.Context(), w)
}

// TestAllNotifications broadcasts test alerts to all active channels.
func (h *Handler) TestAllNotifications(w http.ResponseWriter, r *http.Request) {
	channels, err := h.db.GetActiveNotificationChannels()
	if err != nil || len(channels) == 0 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = components.Toast("No active notification channels found to broadcast to", "error").Render(r.Context(), w)
		return
	}

	successCount := 0
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	for _, ch := range channels {
		if err := h.monitor.GetDispatcher().SendTestAlert(ctx, ch); err == nil {
			successCount++
			_ = h.db.UpdateNotificationChannelLastDispatched(ch.ID)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := fmt.Sprintf("Broadcast test completed: %d of %d active channels confirmed delivery", successCount, len(channels))
	variant := "success"
	if successCount == 0 {
		variant = "error"
	}
	_ = components.Toast(msg, variant).Render(r.Context(), w)
}

// NotificationsJSON returns the list of channels in JSON format with masked sensitive configs.
func (h *Handler) NotificationsJSON(w http.ResponseWriter, r *http.Request) {
	channels, err := h.db.GetNotificationChannels()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type SafeChannel struct {
		ID               string     `json:"id"`
		Name             string     `json:"name"`
		ChannelType      string     `json:"channel_type"`
		Enabled          bool       `json:"enabled"`
		Events           string     `json:"events"`
		CreatedAt        time.Time  `json:"created_at"`
		LastDispatchedAt *time.Time `json:"last_dispatched_at,omitempty"`
	}

	var safe []SafeChannel
	for _, c := range channels {
		safe = append(safe, SafeChannel{
			ID:               c.ID,
			Name:             c.Name,
			ChannelType:      c.ChannelType,
			Enabled:          c.Enabled,
			Events:           c.Events,
			CreatedAt:        c.CreatedAt,
			LastDispatchedAt: c.LastDispatchedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"channels": safe,
		"count":    len(safe),
	})
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

// SatellitesJSON returns all registered edge satellites and their statuses.
func (h *Handler) SatellitesJSON(w http.ResponseWriter, r *http.Request) {
	sats, err := h.db.GetSatellites()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	onlineCount := 0
	regions := make(map[string]int)
	for _, s := range sats {
		if s.Status == "online" {
			onlineCount++
		}
		regions[s.Region]++
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"satellites":   sats,
		"total":        len(sats),
		"online_count": onlineCount,
		"regions":      regions,
	})
}

// SatelliteHeartbeat registers or updates a satellite edge agent.
func (h *Handler) SatelliteHeartbeat(w http.ResponseWriter, r *http.Request) {
	var sat models.Satellite
	if err := json.NewDecoder(r.Body).Decode(&sat); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if sat.ID == "" {
		http.Error(w, "Satellite ID is required", http.StatusBadRequest)
		return
	}
	if sat.Region == "" {
		sat.Region = "default"
	}
	if sat.IPAddress == "" {
		sat.IPAddress = r.RemoteAddr
	}

	if err := h.db.RegisterOrUpdateSatellite(sat); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "registered",
		"id":      sat.ID,
		"message": "Satellite heartbeat recorded successfully",
	})
}

// SatelliteReport ingests probe metrics reported by satellite edge agents.
func (h *Handler) SatelliteReport(w http.ResponseWriter, r *http.Request) {
	var report struct {
		SatelliteID string                  `json:"satellite_id"`
		Region      string                  `json:"region"`
		Probes      []models.SatelliteProbe `json:"probes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, "Invalid probe report JSON", http.StatusBadRequest)
		return
	}

	for _, p := range report.Probes {
		if p.SatelliteID == "" {
			p.SatelliteID = report.SatelliteID
		}
		if p.Region == "" {
			p.Region = report.Region
		}
		_ = h.db.RecordSatelliteProbe(p)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"recorded": len(report.Probes),
	})
}

// SatelliteTargets returns list of active fleet servers that satellite agents should probe.
func (h *Handler) SatelliteTargets(w http.ResponseWriter, r *http.Request) {
	servers, err := h.db.GetAllServers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type ProbeTarget struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Driver      string `json:"driver"`
		EndpointURL string `json:"endpoint_url"`
		Protocol    string `json:"protocol"`
		Region      string `json:"region"`
	}

	var targets []ProbeTarget
	for _, s := range servers {
		if s.Status == "active" || s.Driver == "http" || s.Driver == "tcp" {
			targets = append(targets, ProbeTarget{
				ID:          s.ID,
				Name:        s.Name,
				Driver:      s.Driver,
				EndpointURL: s.EndpointURL,
				Protocol:    s.Protocol,
				Region:      s.Region,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"targets": targets,
		"count":   len(targets),
	})
}

// UpdateServerRunbook updates the runbook webhook URL and active status for a server.
func (h *Handler) UpdateServerRunbook(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID provided", http.StatusBadRequest)
		return
	}

	_ = r.ParseForm()
	runbookURL := strings.TrimSpace(r.FormValue("runbook_url"))
	enabled := r.FormValue("runbook_enabled") == "1"

	if err := h.db.UpdateServerRunbook(id, runbookURL, enabled); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update runbook: %v", err), http.StatusInternalServerError)
		return
	}

	server, err := h.db.GetServerByID(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logs, _ := h.db.GetServerLogs(id, 15)
	remediations, _ := h.db.GetServerRemediations(id, 5)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ServerDetailDrawer(*server, logs, remediations).Render(r.Context(), w)
}

// TriggerServerRunbook executes a manual smoke test of a server's auto-healing runbook.
func (h *Handler) TriggerServerRunbook(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(chi.URLParam(r, "id"))
	if id == "" {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetServerByID(id)
	if err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	if !server.RunbookEnabled || server.RunbookURL == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = components.Toast("Cannot trigger: Runbook is not enabled or URL is empty", "error").Render(r.Context(), w)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if h.monitor != nil && h.monitor.GetHealer() != nil {
			_ = h.monitor.GetHealer().TriggerRemediation(ctx, *server, "Manual Operator Smoke Test")
		}
	}()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := fmt.Sprintf("Remediation smoke test dispatched for %s (%s)", server.Name, server.RunbookURL)
	_ = components.Toast(msg, "success").Render(r.Context(), w)
}

// ExportFleetYAML exports the entire fleet in GitOps-ready fleet.yaml format.
func (h *Handler) ExportFleetYAML(w http.ResponseWriter, r *http.Request) {
	yamlBytes, err := gitops.ExportFleetConfig(h.db)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to export fleet YAML: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"fleet.yaml\"")
	_, _ = w.Write(yamlBytes)
}


