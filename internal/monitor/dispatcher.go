package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"botkeep/internal/models"
)

type AlertEvent struct {
	ServerID    string    `json:"server_id"`
	ServerName  string    `json:"server_name"`
	Driver      string    `json:"driver"`
	EndpointURL string    `json:"endpoint_url"`
	Region      string    `json:"region"`
	Status      string    `json:"status"`
	Recovered   bool      `json:"recovered"`
	Reason      string    `json:"reason"`
	LatencyMS   float64   `json:"latency_ms"`
	Timestamp   time.Time `json:"timestamp"`
}

// Dispatcher manages outbound notification delivery across multiple protocol channels.
type Dispatcher struct {
	client *http.Client
}

func NewDispatcher(timeout time.Duration) *Dispatcher {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Dispatcher{
		client: &http.Client{Timeout: timeout},
	}
}

// Dispatch sends the alert event to the given notification channel.
func (d *Dispatcher) Dispatch(ctx context.Context, ch models.NotificationChannel, ev AlertEvent) error {
	switch strings.ToLower(ch.ChannelType) {
	case "discord":
		return d.dispatchDiscord(ctx, ch, ev)
	case "slack":
		return d.dispatchSlack(ctx, ch, ev)
	case "telegram":
		return d.dispatchTelegram(ctx, ch, ev)
	case "ntfy":
		return d.dispatchNtfy(ctx, ch, ev)
	case "webhook":
		return d.dispatchWebhook(ctx, ch, ev)
	default:
		return fmt.Errorf("unsupported channel type: %s", ch.ChannelType)
	}
}

// SendTestAlert sends a verification ping to the specified channel.
func (d *Dispatcher) SendTestAlert(ctx context.Context, ch models.NotificationChannel) error {
	testEv := AlertEvent{
		ServerID:    "test-canary-01",
		ServerName:  "botkeep-canary-probe",
		Driver:      "http",
		EndpointURL: "https://example.com/health",
		Region:      "us-east-1",
		Status:      "active",
		Recovered:   true,
		Reason:      "Manual test verification dispatched from BotKeep Dashboard",
		LatencyMS:   24.5,
		Timestamp:   time.Now(),
	}
	return d.Dispatch(ctx, ch, testEv)
}

func (d *Dispatcher) dispatchDiscord(ctx context.Context, ch models.NotificationChannel, ev AlertEvent) error {
	var cfg models.DiscordConfig
	if err := json.Unmarshal([]byte(ch.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("invalid discord config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("discord webhook URL is required")
	}

	color := 15673940 // Red (#EF4444) for outage
	title := fmt.Sprintf("🚨 [OUTAGE ALERT] %s is DOWN", ev.ServerName)
	if ev.Recovered {
		color = 1096065 // Green (#10B981) for recovered
		title = fmt.Sprintf("🟢 [RECOVERY] %s is OPERATIONAL", ev.ServerName)
	}

	payload := map[string]any{
		"username": "BotKeep Fleet Monitor",
		"embeds": []map[string]any{
			{
				"title":       title,
				"description": ev.Reason,
				"color":       color,
				"fields": []map[string]any{
					{"name": "Target", "value": ev.ServerName, "inline": true},
					{"name": "Region", "value": ev.Region, "inline": true},
					{"name": "Driver", "value": strings.ToUpper(ev.Driver), "inline": true},
					{"name": "Endpoint", "value": ev.EndpointURL, "inline": false},
					{"name": "Latency", "value": fmt.Sprintf("%.2f ms", ev.LatencyMS), "inline": true},
					{"name": "Status", "value": strings.ToUpper(ev.Status), "inline": true},
				},
				"footer": map[string]any{
					"text": "BotKeep DevOps Fleet Observability",
				},
				"timestamp": ev.Timestamp.Format(time.RFC3339),
			},
		},
	}

	return d.postJSON(ctx, cfg.WebhookURL, payload, nil)
}

func (d *Dispatcher) dispatchSlack(ctx context.Context, ch models.NotificationChannel, ev AlertEvent) error {
	var cfg models.SlackConfig
	if err := json.Unmarshal([]byte(ch.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("invalid slack config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("slack webhook URL is required")
	}

	color := "#EF4444"
	title := fmt.Sprintf("🚨 *[OUTAGE ALERT]* %s is DOWN", ev.ServerName)
	if ev.Recovered {
		color = "#10B981"
		title = fmt.Sprintf("🟢 *[RECOVERY]* %s is OPERATIONAL", ev.ServerName)
	}

	payload := map[string]any{
		"text": title,
		"attachments": []map[string]any{
			{
				"color": color,
				"text":  ev.Reason,
				"fields": []map[string]any{
					{"title": "Target", "value": ev.ServerName, "short": true},
					{"title": "Region", "value": ev.Region, "short": true},
					{"title": "Endpoint", "value": ev.EndpointURL, "short": false},
					{"title": "Latency", "value": fmt.Sprintf("%.2f ms", ev.LatencyMS), "short": true},
					{"title": "Driver", "value": strings.ToUpper(ev.Driver), "short": true},
				},
				"ts": ev.Timestamp.Unix(),
			},
		},
	}

	return d.postJSON(ctx, cfg.WebhookURL, payload, nil)
}

func (d *Dispatcher) dispatchTelegram(ctx context.Context, ch models.NotificationChannel, ev AlertEvent) error {
	var cfg models.TelegramConfig
	if err := json.Unmarshal([]byte(ch.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("invalid telegram config: %w", err)
	}
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("telegram bot token and chat id are required")
	}

	var sb strings.Builder
	if ev.Recovered {
		sb.WriteString("<b>🟢 [BOTKEEP RECOVERY] Service Operational</b>\n\n")
		sb.WriteString(fmt.Sprintf("<b>Target:</b> <code>%s</code>\n", ev.ServerName))
		sb.WriteString(fmt.Sprintf("<b>Region:</b> %s | <b>Driver:</b> %s\n", ev.Region, strings.ToUpper(ev.Driver)))
		sb.WriteString(fmt.Sprintf("<b>Latency:</b> %.2f ms\n", ev.LatencyMS))
		sb.WriteString(fmt.Sprintf("<b>Status:</b> %s\n", strings.ToUpper(ev.Status)))
	} else {
		sb.WriteString("<b>🚨 [BOTKEEP OUTAGE ALERT] Node Down</b>\n\n")
		sb.WriteString(fmt.Sprintf("<b>Target:</b> <code>%s</code>\n", ev.ServerName))
		sb.WriteString(fmt.Sprintf("<b>Reason:</b> %s\n", ev.Reason))
		sb.WriteString(fmt.Sprintf("<b>Endpoint:</b> <code>%s</code>\n", ev.EndpointURL))
		sb.WriteString(fmt.Sprintf("<b>Region:</b> %s | <b>Driver:</b> %s\n", ev.Region, strings.ToUpper(ev.Driver)))
	}
	sb.WriteString(fmt.Sprintf("<b>Time:</b> %s", ev.Timestamp.Format("15:04:05 UTC")))

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	payload := map[string]any{
		"chat_id":    cfg.ChatID,
		"text":       sb.String(),
		"parse_mode": "HTML",
	}

	return d.postJSON(ctx, apiURL, payload, nil)
}

func (d *Dispatcher) dispatchNtfy(ctx context.Context, ch models.NotificationChannel, ev AlertEvent) error {
	var cfg models.NtfyConfig
	if err := json.Unmarshal([]byte(ch.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("invalid ntfy config: %w", err)
	}
	if cfg.Topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}
	server := strings.TrimRight(cfg.Server, "/")
	if server == "" {
		server = "https://ntfy.sh"
	}
	url := fmt.Sprintf("%s/%s", server, cfg.Topic)

	title := fmt.Sprintf("🚨 BotKeep Alert: %s DOWN", ev.ServerName)
	priority := "high"
	tags := "warning,skull"
	if ev.Recovered {
		title = fmt.Sprintf("🟢 BotKeep: %s Recovered", ev.ServerName)
		priority = "default"
		tags = "white_check_mark,rocket"
	}

	bodyText := fmt.Sprintf("%s\nEndpoint: %s\nRegion: %s | Latency: %.1fms\nTime: %s",
		ev.Reason, ev.EndpointURL, ev.Region, ev.LatencyMS, ev.Timestamp.Format("15:04:05"))

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(bodyText))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)
	req.Header.Set("User-Agent", "BotKeep-Dispatcher/2.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy returned HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (d *Dispatcher) dispatchWebhook(ctx context.Context, ch models.NotificationChannel, ev AlertEvent) error {
	var cfg models.WebhookConfig
	if err := json.Unmarshal([]byte(ch.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("invalid webhook config: %w", err)
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook URL is required")
	}

	payload := map[string]any{
		"event":       "botkeep_node_alert",
		"channel_id":  ch.ID,
		"server_id":   ev.ServerID,
		"server_name": ev.ServerName,
		"driver":      ev.Driver,
		"endpoint":    ev.EndpointURL,
		"region":      ev.Region,
		"status":      ev.Status,
		"recovered":   ev.Recovered,
		"reason":      ev.Reason,
		"latency_ms":  ev.LatencyMS,
		"timestamp":   ev.Timestamp.Format(time.RFC3339),
	}

	headers := make(map[string]string)
	if cfg.SecretToken != "" {
		headers["X-BotKeep-Token"] = cfg.SecretToken
		headers["Authorization"] = "Bearer " + cfg.SecretToken
	}
	for k, v := range cfg.Headers {
		headers[k] = v
	}

	return d.postJSON(ctx, cfg.URL, payload, headers)
}

func (d *Dispatcher) postJSON(ctx context.Context, targetURL string, payload any, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "BotKeep-Dispatcher/2.0")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("endpoint returned HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
