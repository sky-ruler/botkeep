package monitor

import (
	"context"
	"testing"
	"time"
)

func TestEncodeDecodeGRPCHealth(t *testing.T) {
	req := EncodeGRPCHealthRequest("auth.AuthService")
	if len(req) < 5 {
		t.Fatalf("expected at least 5 bytes, got %d", len(req))
	}
	if req[0] != 0x00 {
		t.Errorf("expected compression flag 0x00, got 0x%x", req[0])
	}

	// Mock SERVING response: 5 byte frame header + 0x08 0x01
	resp := []byte{0x00, 0x00, 0x00, 0x00, 0x02, 0x08, 0x01}
	status, err := DecodeGRPCHealthResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error decoding response: %v", err)
	}
	if status != "SERVING" {
		t.Errorf("expected SERVING, got %s", status)
	}

	// Mock NOT_SERVING response: 5 byte frame header + 0x08 0x02
	respNotServing := []byte{0x00, 0x00, 0x00, 0x00, 0x02, 0x08, 0x02}
	statusNS, err := DecodeGRPCHealthResponse(respNotServing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusNS != "NOT_SERVING" {
		t.Errorf("expected NOT_SERVING, got %s", statusNS)
	}
}

func TestCleanHostPort(t *testing.T) {
	tests := []struct {
		input       string
		defaultPort string
		wantHost    string
		wantHostPort string
	}{
		{"https://cloudflare.com/api", "443", "cloudflare.com", "cloudflare.com:443"},
		{"api.github.com:8443", "443", "api.github.com", "api.github.com:8443"},
		{"grpc://127.0.0.1:50051", "50051", "127.0.0.1", "127.0.0.1:50051"},
		{"localhost", "80", "localhost", "localhost:80"},
	}

	for _, tc := range tests {
		h, hp := CleanHostPort(tc.input, tc.defaultPort)
		if h != tc.wantHost || hp != tc.wantHostPort {
			t.Errorf("CleanHostPort(%q, %q) = (%q, %q), want (%q, %q)",
				tc.input, tc.defaultPort, h, hp, tc.wantHost, tc.wantHostPort)
		}
	}
}

func TestInspectDNS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dns := InspectDNS(ctx, "https://127.0.0.1:8080", 2*time.Second)
	if dns == nil {
		t.Fatal("expected DNSDetails not nil")
	}
	if !dns.Success {
		t.Logf("DNS note: %v", dns.Error)
	}
}
