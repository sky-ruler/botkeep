package models

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func setupTestDBForPush(t *testing.T) *DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_push.db")
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	return db
}

func TestWebPushSubscription_CRUD(t *testing.T) {
	db := setupTestDBForPush(t)
	defer db.Close()

	sub := WebPushSubscription{
		ID:        "sub-desktop-01",
		Endpoint:  "https://push.example.com/v1/sub-12345",
		P256DH:    "test-p256dh-key-data",
		Auth:      "test-auth-secret",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
	}

	// 1. Save subscription
	if err := db.SaveWebPushSubscription(sub); err != nil {
		t.Fatalf("SaveWebPushSubscription failed: %v", err)
	}

	// 2. Count subscriptions
	count, err := db.CountWebPushSubscriptions()
	if err != nil {
		t.Fatalf("CountWebPushSubscriptions failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// 3. Get subscriptions list
	subs, err := db.GetWebPushSubscriptions()
	if err != nil {
		t.Fatalf("GetWebPushSubscriptions failed: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].Endpoint != sub.Endpoint {
		t.Errorf("expected endpoint %q, got %q", sub.Endpoint, subs[0].Endpoint)
	}
	if subs[0].UserAgent != sub.UserAgent {
		t.Errorf("expected user agent %q, got %q", sub.UserAgent, subs[0].UserAgent)
	}

	// 4. Get by ID
	fetched, err := db.GetWebPushSubscriptionByID(sub.ID)
	if err != nil {
		t.Fatalf("GetWebPushSubscriptionByID failed: %v", err)
	}
	if fetched.ID != sub.ID {
		t.Errorf("expected ID %q, got %q", sub.ID, fetched.ID)
	}

	// 5. Update same endpoint (upsert)
	subUpdated := sub
	subUpdated.P256DH = "new-p256dh-key"
	if err := db.SaveWebPushSubscription(subUpdated); err != nil {
		t.Fatalf("SaveWebPushSubscription update failed: %v", err)
	}
	countAfterUpdate, _ := db.CountWebPushSubscriptions()
	if countAfterUpdate != 1 {
		t.Errorf("expected count to remain 1 after upsert, got %d", countAfterUpdate)
	}
	fetchedUpdated, _ := db.GetWebPushSubscriptionByID(sub.ID)
	if fetchedUpdated.P256DH != "new-p256dh-key" {
		t.Errorf("expected updated P256DH, got %q", fetchedUpdated.P256DH)
	}

	// 6. Touch subscription
	if err := db.TouchWebPushSubscription(sub.ID); err != nil {
		t.Fatalf("TouchWebPushSubscription failed: %v", err)
	}
	touched, _ := db.GetWebPushSubscriptionByID(sub.ID)
	if touched.LastNotifiedAt == nil {
		t.Errorf("expected LastNotifiedAt to be non-nil after touch")
	}

	// 7. Delete by endpoint
	if err := db.DeleteWebPushSubscription(sub.Endpoint); err != nil {
		t.Fatalf("DeleteWebPushSubscription failed: %v", err)
	}
	finalCount, _ := db.CountWebPushSubscriptions()
	if finalCount != 0 {
		t.Errorf("expected count 0 after deletion, got %d", finalCount)
	}
}

func TestWebPushSubscription_DeleteByID(t *testing.T) {
	db := setupTestDBForPush(t)
	defer db.Close()

	sub := WebPushSubscription{
		ID:        "sub-mobile-02",
		Endpoint:  "https://push.example.com/v1/sub-99999",
		P256DH:    "mobile-key",
		Auth:      "mobile-auth",
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)",
	}
	_ = db.SaveWebPushSubscription(sub)

	if err := db.DeleteWebPushSubscriptionByID("sub-mobile-02"); err != nil {
		t.Fatalf("DeleteWebPushSubscriptionByID failed: %v", err)
	}
	count, _ := db.CountWebPushSubscriptions()
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestWebPushSubscription_BroadcastAndExpiredCleanup(t *testing.T) {
	db := setupTestDBForPush(t)
	defer db.Close()

	received := 0
	// Mock push service server that responds 200 for sub1, and 410 Gone for sub2
	mockPushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/expired" {
			w.WriteHeader(http.StatusGone) // Expired subscription
			return
		}
		received++
		w.WriteHeader(http.StatusCreated)
	}))
	defer mockPushServer.Close()

	subActive := WebPushSubscription{
		ID:       "sub-active",
		Endpoint: mockPushServer.URL + "/active",
		P256DH:   "active-key",
		Auth:     "active-auth",
	}
	subExpired := WebPushSubscription{
		ID:       "sub-expired",
		Endpoint: mockPushServer.URL + "/expired",
		P256DH:   "expired-key",
		Auth:     "expired-auth",
	}

	_ = db.SaveWebPushSubscription(subActive)
	_ = db.SaveWebPushSubscription(subExpired)

	dispatched, err := db.BroadcastWebPushNotification("Outage Alert", "Server down", "/status")
	if err != nil {
		t.Fatalf("BroadcastWebPushNotification failed: %v", err)
	}
	if dispatched != 1 {
		t.Errorf("expected 1 successful dispatched notification, got %d", dispatched)
	}
	if received != 1 {
		t.Errorf("expected 1 successful push delivery, got %d", received)
	}

	// Verify that sub-expired was automatically pruned due to 410 Gone
	subs, _ := db.GetWebPushSubscriptions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 remaining subscription after pruning expired endpoint, got %d", len(subs))
	}
	if subs[0].ID != "sub-active" {
		t.Errorf("expected remaining subscription to be sub-active, got %s", subs[0].ID)
	}
}
