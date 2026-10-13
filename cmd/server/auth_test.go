package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	authUser := "admin"
	authPass := "supersecret123"
	apiKey := "bk_live_key_998877"

	mw := NewAuthMiddleware(authUser, authPass, apiKey)

	// Mock next handler
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ACCESS_GRANTED"))
	})
	protectedRouter := mw(okHandler)

	tests := []struct {
		name           string
		path           string
		acceptHeader   string
		authHeader     string
		apiKeyHeader   string
		basicUser      string
		basicPass      string
		expectedStatus int
		expectAuthWWW  bool
	}{
		{
			name:           "Public /status without auth",
			path:           "/status",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Public /api/status without auth",
			path:           "/api/status",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Public /healthz without auth",
			path:           "/healthz",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Public /metrics/prometheus without auth",
			path:           "/metrics/prometheus",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Protected / without credentials (browser text/html)",
			path:           "/",
			acceptHeader:   "text/html,application/xhtml+xml",
			expectedStatus: http.StatusUnauthorized,
			expectAuthWWW:  true,
		},
		{
			name:           "Protected /api/fleet without credentials (API json)",
			path:           "/api/fleet/export",
			acceptHeader:   "application/json",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Protected / with valid HTTP Basic Auth",
			path:           "/servers",
			basicUser:      "admin",
			basicPass:      "supersecret123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Protected / with incorrect HTTP Basic Auth",
			path:           "/servers",
			basicUser:      "admin",
			basicPass:      "wrongpassword",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Protected / with valid Bearer Token",
			path:           "/servers",
			authHeader:     "Bearer bk_live_key_998877",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Protected / with valid X-API-Key header",
			path:           "/api/fleet/export",
			apiKeyHeader:   "bk_live_key_998877",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Protected / with invalid API key",
			path:           "/api/fleet/export",
			apiKeyHeader:   "invalid_key_random",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			if tt.apiKeyHeader != "" {
				req.Header.Set("X-API-Key", tt.apiKeyHeader)
			}
			if tt.basicUser != "" {
				req.SetBasicAuth(tt.basicUser, tt.basicPass)
			}

			w := httptest.NewRecorder()
			protectedRouter.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Path %s expected status %d, got %d", tt.path, tt.expectedStatus, w.Code)
			}

			if tt.expectAuthWWW {
				authResp := w.Header().Get("WWW-Authenticate")
				if !strings.Contains(authResp, "Basic realm=") {
					t.Errorf("Expected WWW-Authenticate header, got %q", authResp)
				}
			}
		})
	}
}
