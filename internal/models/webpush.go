package models

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// WebPushSubscription represents an active browser device push subscription registered with the service worker.
type WebPushSubscription struct {
	ID             string     `json:"id"`
	Endpoint       string     `json:"endpoint"`
	P256DH         string     `json:"p256dh"`
	Auth           string     `json:"auth"`
	UserAgent      string     `json:"user_agent"`
	CreatedAt      time.Time  `json:"created_at"`
	LastNotifiedAt *time.Time `json:"last_notified_at,omitempty"`
}

// PushNotificationPayload represents the standardized JSON structure received by the service worker push event.
type PushNotificationPayload struct {
	Title     string                 `json:"title"`
	Body      string                 `json:"body"`
	Icon      string                 `json:"icon"`
	Badge     string                 `json:"badge"`
	URL       string                 `json:"url"`
	Tag       string                 `json:"tag"`
	Timestamp int64                  `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// GeneratePushID generates a random hex identifier for a subscription.
func GeneratePushID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "push-" + hex.EncodeToString(b)
}

// SaveWebPushSubscription stores or updates an active browser push subscription.
func (db *DB) SaveWebPushSubscription(sub WebPushSubscription) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if sub.ID == "" {
		sub.ID = GeneratePushID()
	}

	query := `
		INSERT INTO web_push_subscriptions (id, endpoint, p256dh, auth, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_agent = excluded.user_agent,
			created_at = CURRENT_TIMESTAMP;
	`
	_, err := db.conn.Exec(query, sub.ID, sub.Endpoint, sub.P256DH, sub.Auth, sub.UserAgent)
	if err != nil {
		return fmt.Errorf("failed to save web push subscription: %w", err)
	}

	return nil
}

// GetWebPushSubscriptions returns all registered browser push subscriptions.
func (db *DB) GetWebPushSubscriptions() ([]WebPushSubscription, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	query := `
		SELECT id, endpoint, p256dh, auth, user_agent, created_at, last_notified_at
		FROM web_push_subscriptions
		ORDER BY created_at DESC;
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list web push subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []WebPushSubscription
	for rows.Next() {
		var s WebPushSubscription
		var lastNotified sql.NullTime
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.P256DH, &s.Auth, &s.UserAgent, &s.CreatedAt, &lastNotified); err != nil {
			return nil, err
		}
		if lastNotified.Valid {
			s.LastNotifiedAt = &lastNotified.Time
		}
		subs = append(subs, s)
	}

	return subs, nil
}

// GetWebPushSubscriptionByID retrieves a single subscription by ID.
func (db *DB) GetWebPushSubscriptionByID(id string) (*WebPushSubscription, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	query := `
		SELECT id, endpoint, p256dh, auth, user_agent, created_at, last_notified_at
		FROM web_push_subscriptions
		WHERE id = ?;
	`
	var s WebPushSubscription
	var lastNotified sql.NullTime
	err := db.conn.QueryRow(query, id).Scan(&s.ID, &s.Endpoint, &s.P256DH, &s.Auth, &s.UserAgent, &s.CreatedAt, &lastNotified)
	if err != nil {
		return nil, err
	}
	if lastNotified.Valid {
		s.LastNotifiedAt = &lastNotified.Time
	}
	return &s, nil
}

// DeleteWebPushSubscription removes a subscription by endpoint URL.
func (db *DB) DeleteWebPushSubscription(endpoint string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.conn.Exec("DELETE FROM web_push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

// DeleteWebPushSubscriptionByID removes a subscription by ID.
func (db *DB) DeleteWebPushSubscriptionByID(id string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.conn.Exec("DELETE FROM web_push_subscriptions WHERE id = ?", id)
	return err
}

// CountWebPushSubscriptions returns the total number of subscribed devices.
func (db *DB) CountWebPushSubscriptions() (int, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM web_push_subscriptions").Scan(&count)
	return count, err
}

// TouchWebPushSubscription marks the last notified timestamp.
func (db *DB) TouchWebPushSubscription(id string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.conn.Exec("UPDATE web_push_subscriptions SET last_notified_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// BroadcastWebPushNotification dispatches a push notification to all subscribed devices.
func (db *DB) BroadcastWebPushNotification(title, body, targetURL string) (int, error) {
	subs, err := db.GetWebPushSubscriptions()
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	payload := PushNotificationPayload{
		Title:     title,
		Body:      body,
		Icon:      "/static/icons/icon-192.png",
		Badge:     "/static/icons/icon-192.png",
		URL:       targetURL,
		Tag:       fmt.Sprintf("botkeep-%d", time.Now().Unix()),
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"url": targetURL,
		},
	}

	payloadBytes, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 5 * time.Second}
	dispatched := 0

	for _, sub := range subs {
		// If endpoint is a valid HTTP URL (e.g. FCM, Mozilla Autopush, or internal webhook)
		if strings.HasPrefix(sub.Endpoint, "http://") || strings.HasPrefix(sub.Endpoint, "https://") {
			req, reqErr := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(payloadBytes))
			if reqErr == nil {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("TTL", "60")
				// Send notification attempt (ignore delivery failures to offline client push gateways)
				resp, postErr := client.Do(req)
				if postErr == nil {
					_ = resp.Body.Close()
					// HTTP 410 Gone or 404 Not Found indicates subscription expired / revoked
					if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
						_ = db.DeleteWebPushSubscriptionByID(sub.ID)
						continue
					}
				}
			}
		}

		_ = db.TouchWebPushSubscription(sub.ID)
		dispatched++
	}

	// Record audit entry
	_ = db.AddLog("", "INFO", fmt.Sprintf("[WebPush] Broadcast alert dispatched to %d device(s): %s - %s", dispatched, title, body))

	return dispatched, nil
}
