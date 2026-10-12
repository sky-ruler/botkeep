package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"botkeep/internal/models"
)

// Healer coordinates active auto-remediation webhook runbooks when fleet nodes fail.
type Healer struct {
	client   *http.Client
	db       *models.DB
	mu       sync.Mutex
	cooldown map[string]time.Time // serverID -> cooldown expiry (prevents webhook storms)
}

func NewHealer(db *models.DB, timeout time.Duration) *Healer {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Healer{
		client:   &http.Client{Timeout: timeout},
		db:       db,
		cooldown: make(map[string]time.Time),
	}
}

// TriggerRemediation evaluates and executes an auto-healing runbook for a failing server.
func (h *Healer) TriggerRemediation(ctx context.Context, srv models.Server, reason string) error {
	if !srv.RunbookEnabled || srv.RunbookURL == "" {
		return nil
	}

	h.mu.Lock()
	lastTrigger, inCooldown := h.cooldown[srv.ID]
	if inCooldown && time.Now().Before(lastTrigger.Add(3*time.Minute)) {
		h.mu.Unlock()
		log.Printf("[Healer] Runbook cooldown active for %s (%s). Skipping repeat execution.", srv.Name, srv.ID)
		return nil
	}
	h.cooldown[srv.ID] = time.Now()
	h.mu.Unlock()

	// Record initial remediation action in progress
	rem, err := h.db.RecordRemediation(models.Remediation{
		ServerID:      srv.ID,
		ActionName:    "Automated Runbook Webhook",
		TriggerReason: reason,
		Status:        "in_progress",
		ResponseCode:  0,
		OutputLog:     "Dispatching remediation payload to endpoint...",
	})
	if err != nil {
		log.Printf("[Healer] Failed to record remediation entry: %v", err)
	}

	logMsg := fmt.Sprintf("[%s] AUTO-HEAL TRIGGER: Health probe failed (%s). Executing runbook webhook at %s...", srv.Name, reason, srv.RunbookURL)
	_ = h.db.AddLog(srv.ID, "WARN", logMsg)

	// Execute HTTP Webhook payload
	payload, _ := json.Marshal(map[string]any{
		"event":          "botkeep.incident.auto_heal",
		"server_id":      srv.ID,
		"server_name":    srv.Name,
		"endpoint_url":   srv.EndpointURL,
		"region":         srv.Region,
		"driver":         srv.Driver,
		"failure_reason": reason,
		"attempts":       srv.AutoHealAttempts + 1,
		"timestamp":      time.Now().UTC(),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.RunbookURL, bytes.NewReader(payload))
	if err != nil {
		if rem != nil {
			_ = h.db.UpdateRemediationStatus(rem.ID, "failed", 0, fmt.Sprintf("Failed to construct request: %v", err), false)
		}
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "BotKeep-AutoHeal/2.0")

	resp, err := h.client.Do(req)
	if err != nil {
		errMsg := fmt.Sprintf("Runbook webhook dispatch failed: %v", err)
		if rem != nil {
			_ = h.db.UpdateRemediationStatus(rem.ID, "failed", 0, errMsg, false)
		}
		_ = h.db.AddLog(srv.ID, "ERROR", fmt.Sprintf("[%s] AUTO-HEAL ERROR: %s", srv.Name, errMsg))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		successLog := fmt.Sprintf("Runbook webhook acknowledged (HTTP %d). Awaiting node recovery probe...", resp.StatusCode)
		if rem != nil {
			_ = h.db.UpdateRemediationStatus(rem.ID, "triggered", resp.StatusCode, successLog, false)
		}
		_ = h.db.AddLog(srv.ID, "INFO", fmt.Sprintf("[%s] AUTO-HEAL DISPATCHED: %s", srv.Name, successLog))
	} else {
		errLog := fmt.Sprintf("Runbook webhook returned error code: HTTP %d", resp.StatusCode)
		if rem != nil {
			_ = h.db.UpdateRemediationStatus(rem.ID, "failed", resp.StatusCode, errLog, false)
		}
		_ = h.db.AddLog(srv.ID, "WARN", fmt.Sprintf("[%s] AUTO-HEAL WARN: %s", srv.Name, errLog))
	}

	return nil
}

// OnRecovery resolves any pending remediation and records successful self-healing.
func (h *Healer) OnRecovery(srv models.Server) {
	if !srv.RunbookEnabled {
		return
	}

	// Update any open/triggered remediation to healed
	_ = h.db.RecordAutoHealSuccess(srv.ID)

	healMsg := fmt.Sprintf("[%s] AUTO-HEALED: Node operational following automated runbook execution! Incident mitigated without human intervention.", srv.Name)
	_ = h.db.AddLog(srv.ID, "DEPLOY", healMsg)
	log.Println("[Healer]", healMsg)
}
