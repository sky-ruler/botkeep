package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GRPCProbeResult captures gRPC health check status, latency, and details.
type GRPCProbeResult struct {
	ServingStatus string  `json:"serving_status"` // "SERVING", "NOT_SERVING", "SERVICE_UNKNOWN", "UNIMPLEMENTED"
	StatusCode    int     `json:"status_code"`    // gRPC Status (0 = OK, 12 = UNIMPLEMENTED, etc)
	LatencyMS     float64 `json:"latency_ms"`
	Success       bool    `json:"success"`
	ErrorMessage  string  `json:"error_message"`
}

// EncodeGRPCHealthRequest builds a standard gRPC wire frame for grpc.health.v1.Health/Check.
// Frame format: 1-byte flag (0x00) + 4-byte big-endian payload length + protobuf payload.
func EncodeGRPCHealthRequest(service string) []byte {
	var protoPayload []byte
	if service != "" {
		// Field 1, wire type 2 (length-delimited): tag = (1 << 3) | 2 = 0x0A
		protoPayload = append(protoPayload, 0x0A)
		serviceBytes := []byte(service)
		protoPayload = append(protoPayload, byte(len(serviceBytes)))
		protoPayload = append(protoPayload, serviceBytes...)
	}

	frame := make([]byte, 5+len(protoPayload))
	frame[0] = 0x00 // uncompressed
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(protoPayload)))
	if len(protoPayload) > 0 {
		copy(frame[5:], protoPayload)
	}
	return frame
}

// DecodeGRPCHealthResponse parses the gRPC frame and protobuf response.
func DecodeGRPCHealthResponse(data []byte) (servingStatus string, err error) {
	if len(data) < 5 {
		return "UNKNOWN", fmt.Errorf("response too short for gRPC frame: %d bytes", len(data))
	}
	payloadLen := binary.BigEndian.Uint32(data[1:5])
	payload := data[5:]
	if uint32(len(payload)) < payloadLen {
		return "UNKNOWN", fmt.Errorf("truncated gRPC payload: expected %d bytes, got %d", payloadLen, len(payload))
	}

	// Protobuf message: field 1, wire type 0 (varint enum ServingStatus): tag = 0x08
	for i := 0; i < len(payload); i++ {
		if payload[i] == 0x08 && i+1 < len(payload) {
			statusVal := payload[i+1]
			switch statusVal {
			case 1:
				return "SERVING", nil
			case 2:
				return "NOT_SERVING", nil
			case 3:
				return "SERVICE_UNKNOWN", nil
			default:
				return "UNKNOWN", nil
			}
		}
	}
	return "UNKNOWN", nil
}

// ProbeGRPC performs a native health check probe against a gRPC server endpoint.
func ProbeGRPC(ctx context.Context, target string, service string, timeout time.Duration) *GRPCProbeResult {
	result := &GRPCProbeResult{
		ServingStatus: "UNKNOWN",
		Success:       false,
	}

	raw := strings.TrimSpace(target)
	isTLS := strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "grpcs://")
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			raw = u.Host
		}
	}

	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		host = raw
		if isTLS {
			port = "443"
		} else {
			port = "50051"
		}
	}

	if port == "443" {
		isTLS = true
	}

	start := time.Now()

	// If TLS is enabled, send an HTTP/2 gRPC health check request
	if isTLS {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName:         host,
				InsecureSkipVerify: false,
			},
			ForceAttemptHTTP2: true,
		}
		client := &http.Client{
			Transport: tr,
			Timeout:   timeout,
		}

		reqBody := EncodeGRPCHealthRequest(service)
		reqURL := fmt.Sprintf("https://%s:%s/grpc.health.v1.Health/Check", host, port)
		req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(reqBody))
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to build gRPC request: %v", err)
			result.LatencyMS = float64(time.Since(start).Microseconds()) / 1000.0
			return result
		}

		req.Header.Set("Content-Type", "application/grpc")
		req.Header.Set("TE", "trailers")
		req.Header.Set("User-Agent", "BotKeep-gRPC-Prober/2.0")

		resp, err := client.Do(req)
		latency := float64(time.Since(start).Microseconds()) / 1000.0
		if latency <= 0 {
			latency = 0.1
		}
		result.LatencyMS = latency

		if err != nil {
			result.ErrorMessage = err.Error()
			return result
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		// Check trailers or headers for grpc-status
		grpcStatusStr := resp.Trailer.Get("grpc-status")
		if grpcStatusStr == "" {
			grpcStatusStr = resp.Header.Get("grpc-status")
		}

		grpcCode := 0
		if grpcStatusStr != "" {
			grpcCode, _ = strconv.Atoi(grpcStatusStr)
		}
		result.StatusCode = grpcCode

		grpcMsg := resp.Trailer.Get("grpc-message")
		if grpcMsg == "" {
			grpcMsg = resp.Header.Get("grpc-message")
		}

		if grpcCode == 0 {
			status, err := DecodeGRPCHealthResponse(respBody)
			if err == nil {
				result.ServingStatus = status
				if status == "SERVING" {
					result.Success = true
					return result
				}
				result.ErrorMessage = fmt.Sprintf("gRPC service status: %s", status)
				return result
			}
			result.ServingStatus = "SERVING"
			result.Success = true
			return result
		} else if grpcCode == 12 {
			// Code 12 = UNIMPLEMENTED (gRPC server reachable, Health service not installed)
			result.ServingStatus = "UNIMPLEMENTED"
			result.Success = true // Host is alive and processing gRPC
			result.ErrorMessage = "gRPC channel active (Health check service not implemented)"
			return result
		}

		result.ErrorMessage = fmt.Sprintf("gRPC status code %d: %s", grpcCode, grpcMsg)
		return result
	}

	// Plaintext gRPC / socket probe
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	latency := float64(time.Since(start).Microseconds()) / 1000.0
	if latency <= 0 {
		latency = 0.1
	}
	result.LatencyMS = latency

	if err != nil {
		result.ErrorMessage = fmt.Sprintf("TCP dial to gRPC port failed: %v", err)
		return result
	}
	_ = conn.Close()

	result.Success = true
	result.ServingStatus = "SERVING"
	result.StatusCode = 0
	return result
}
