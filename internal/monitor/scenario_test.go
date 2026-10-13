package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"botkeep/internal/models"
)

func TestParseScenarioSteps(t *testing.T) {
	// 1. Empty string
	steps, err := ParseScenarioSteps("")
	if err != nil || steps != nil {
		t.Fatalf("Expected nil steps for empty string, got: %v, %v", steps, err)
	}

	// 2. Valid JSON array
	validJSON := `[
		{"name":"Ping","method":"GET","url":"https://example.com","expected_status":200,"assertion":"welcome"},
		{"name":"Auth","method":"POST","url":"https://example.com/login","expected_status":201}
	]`
	steps, err = ParseScenarioSteps(validJSON)
	if err != nil {
		t.Fatalf("Failed to parse valid steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("Expected 2 steps, got %d", len(steps))
	}
	if steps[0].Name != "Ping" || steps[0].Assertion != "welcome" {
		t.Errorf("Step 0 mismatch: %+v", steps[0])
	}
	if steps[1].Method != "POST" || steps[1].ExpectedStatus != 201 {
		t.Errorf("Step 1 mismatch: %+v", steps[1])
	}

	// 3. Invalid JSON syntax
	_, err = ParseScenarioSteps(`[invalid json`)
	if err == nil {
		t.Errorf("Expected error for invalid JSON, got nil")
	}
}

func TestExecuteScenarioSuccess(t *testing.T) {
	// Mock server that tracks session cookies across multi-step transactions
	cookieChecked := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{
				Name:  "session_token",
				Value: "live-session-jwt",
				Path:  "/",
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"logged_in","user_id":"admin"}`))
		case "/dashboard":
			c, err := r.Cookie("session_token")
			if err == nil && c.Value == "live-session-jwt" {
				cookieChecked = true
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"account":"active","role":"cluster-admin"}`))
		case "/metrics":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`all_systems_green`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	steps := []models.SyntheticStep{
		{
			Name:           "Authenticate Session",
			Method:         "POST",
			URL:            ts.URL + "/login",
			ExpectedStatus: 200,
			Assertion:      "logged_in",
		},
		{
			Name:           "Verify Dashboard State",
			Method:         "GET",
			URL:            ts.URL + "/dashboard",
			ExpectedStatus: 200,
			Assertion:      "cluster-admin",
		},
		{
			Name:           "Check Deep Telemetry",
			Method:         "GET",
			URL:            ts.URL + "/metrics",
			ExpectedStatus: 200,
			Assertion:      "all_systems_green",
		},
	}

	res := ExecuteScenario(context.Background(), steps, 5*time.Second)
	if !res.Passed {
		t.Fatalf("Expected scenario to pass, failed with: %s", res.ErrorMessage)
	}
	if res.TotalSteps != 3 {
		t.Errorf("Expected 3 total steps, got %d", res.TotalSteps)
	}
	if res.FailedStep != 0 {
		t.Errorf("Expected failed step 0, got %d", res.FailedStep)
	}
	if !cookieChecked {
		t.Errorf("Expected session cookie to be persisted across scenario steps")
	}
	if res.TotalLatency <= 0 {
		t.Errorf("Expected positive latency, got %f", res.TotalLatency)
	}
	for i, s := range res.StepResults {
		if !s.Success {
			t.Errorf("Step %d reported failure", i+1)
		}
	}
}

func TestExecuteScenarioStepFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/step1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		case "/step2":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`upstream timeout`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	steps := []models.SyntheticStep{
		{Name: "Step 1 OK", Method: "GET", URL: ts.URL + "/step1", ExpectedStatus: 200},
		{Name: "Step 2 Fail", Method: "GET", URL: ts.URL + "/step2", ExpectedStatus: 200},
	}

	res := ExecuteScenario(context.Background(), steps, 5*time.Second)
	if res.Passed {
		t.Fatalf("Expected scenario to fail at step 2, but it passed")
	}
	if res.FailedStep != 2 {
		t.Errorf("Expected FailedStep 2, got %d", res.FailedStep)
	}
}

func TestExecuteScenarioAssertionFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"maintenance"}`))
	}))
	defer ts.Close()

	steps := []models.SyntheticStep{
		{
			Name:           "Check Status",
			Method:         "GET",
			URL:            ts.URL,
			ExpectedStatus: 200,
			Assertion:      "all_systems_green",
		},
	}

	res := ExecuteScenario(context.Background(), steps, 5*time.Second)
	if res.Passed {
		t.Fatalf("Expected assertion keyword failure, but scenario passed")
	}
	if res.FailedStep != 1 {
		t.Errorf("Expected FailedStep 1, got %d", res.FailedStep)
	}
}
