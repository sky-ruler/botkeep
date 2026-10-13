package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"botkeep/internal/models"
)

// ExecuteScenario runs a multi-step synthetic HTTP transaction flow against a node.
// It maintains session cookies across steps (e.g. login -> session cookie -> authenticated query).
func ExecuteScenario(ctx context.Context, steps []models.SyntheticStep, timeout time.Duration) *models.ScenarioResult {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	res := &models.ScenarioResult{
		TotalSteps: len(steps),
		Passed:     true,
	}

	for i, step := range steps {
		stepIdx := i + 1
		method := strings.ToUpper(strings.TrimSpace(step.Method))
		if method == "" {
			method = "GET"
		}
		expectedStatus := step.ExpectedStatus
		if expectedStatus == 0 {
			expectedStatus = 200
		}

		var bodyReader io.Reader
		if step.Body != "" {
			bodyReader = bytes.NewBufferString(step.Body)
		}

		req, err := http.NewRequestWithContext(ctx, method, step.URL, bodyReader)
		if err != nil {
			stepRes := models.StepProbeResult{
				StepIndex:   stepIdx,
				Name:        step.Name,
				Success:     false,
				ErrorReason: fmt.Sprintf("Failed to construct request: %v", err),
			}
			res.StepResults = append(res.StepResults, stepRes)
			res.Passed = false
			res.FailedStep = stepIdx
			res.ErrorMessage = fmt.Sprintf("Step %d (%s) failed: %v", stepIdx, step.Name, err)
			return res
		}

		req.Header.Set("User-Agent", "BotKeep-Synthetic-Scenario/2.0")
		if step.Body != "" && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}

		for k, v := range step.Headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := client.Do(req)
		latency := float64(time.Since(start).Microseconds()) / 1000.0
		if latency <= 0 {
			latency = 0.1
		}
		res.TotalLatency += latency

		if err != nil {
			stepRes := models.StepProbeResult{
				StepIndex:   stepIdx,
				Name:        step.Name,
				LatencyMS:   latency,
				Success:     false,
				ErrorReason: err.Error(),
			}
			res.StepResults = append(res.StepResults, stepRes)
			res.Passed = false
			res.FailedStep = stepIdx
			res.ErrorMessage = fmt.Sprintf("Step %d (%s) network error: %v", stepIdx, step.Name, err)
			return res
		}

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		_ = resp.Body.Close()
		bodyText := string(bodyBytes)

		// Check status code
		statusOk := resp.StatusCode == expectedStatus
		// Check body assertion if configured
		assertionOk := true
		if step.Assertion != "" {
			assertionOk = strings.Contains(bodyText, step.Assertion)
		}

		if !statusOk || !assertionOk {
			errReason := ""
			if !statusOk {
				errReason = fmt.Sprintf("Status %d (expected %d)", resp.StatusCode, expectedStatus)
			} else {
				errReason = fmt.Sprintf("Assertion keyword %q missing in response", step.Assertion)
			}

			stepRes := models.StepProbeResult{
				StepIndex:   stepIdx,
				Name:        step.Name,
				StatusCode:  resp.StatusCode,
				LatencyMS:   latency,
				Success:     false,
				ErrorReason: errReason,
			}
			res.StepResults = append(res.StepResults, stepRes)
			res.Passed = false
			res.FailedStep = stepIdx
			res.ErrorMessage = fmt.Sprintf("Step %d (%s) assertion failed: %s", stepIdx, step.Name, errReason)
			return res
		}

		stepRes := models.StepProbeResult{
			StepIndex:  stepIdx,
			Name:       step.Name,
			StatusCode: resp.StatusCode,
			LatencyMS:  latency,
			Success:    true,
		}
		res.StepResults = append(res.StepResults, stepRes)
	}

	return res
}

// ParseScenarioSteps parses a JSON string into a slice of SyntheticStep items.
func ParseScenarioSteps(raw string) ([]models.SyntheticStep, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var steps []models.SyntheticStep
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return nil, err
	}
	return steps, nil
}
