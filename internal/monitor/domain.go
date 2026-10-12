package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// SSLCertDetails captures cryptographic TLS certificate parameters.
type SSLCertDetails struct {
	CommonName     string    `json:"common_name"`
	Issuer         string    `json:"issuer"`
	SANs           []string  `json:"sans"`
	ValidFrom      time.Time `json:"valid_from"`
	ValidTo        time.Time `json:"valid_to"`
	DaysRemaining  int       `json:"days_remaining"`
	TLSVersion     string    `json:"tls_version"`
	CipherSuite    string    `json:"cipher_suite"`
	IsExpired      bool      `json:"is_expired"`
	IsExpiringSoon bool      `json:"is_expiring_soon"` // true if <= 14 days
}

// DNSDetails captures DNS domain resolution health and latency.
type DNSDetails struct {
	Domain          string   `json:"domain"`
	LookupLatencyMS float64  `json:"lookup_latency_ms"`
	IPs             []string `json:"ips"`
	CNAME           string   `json:"cname"`
	Success         bool     `json:"success"`
	Error           string   `json:"error"`
}

// CleanHostPort parses an endpoint URL or host:port into standard host and host:port components.
func CleanHostPort(rawTarget string, defaultPort string) (host string, hostPort string) {
	raw := strings.TrimSpace(rawTarget)
	if strings.Contains(raw, "://") {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			raw = parsed.Host
		}
	}

	// Remove trailing paths or slashes
	if idx := strings.Index(raw, "/"); idx != -1 {
		raw = raw[:idx]
	}

	if h, p, err := net.SplitHostPort(raw); err == nil {
		return h, net.JoinHostPort(h, p)
	}

	return raw, net.JoinHostPort(raw, defaultPort)
}

// InspectSSL connects to a remote TLS endpoint and extracts full certificate telemetry.
func InspectSSL(ctx context.Context, rawTarget string, timeout time.Duration) (*SSLCertDetails, error) {
	host, hostPort := CleanHostPort(rawTarget, "443")
	if host == "" {
		return nil, fmt.Errorf("empty host target for SSL probe")
	}

	dialer := &net.Dialer{
		Timeout: timeout,
	}

	tlsConfig := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: false,
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", hostPort, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("TLS handshake failed for %s: %w", hostPort, err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no peer certificates presented by %s", hostPort)
	}

	leaf := state.PeerCertificates[0]
	now := time.Now()
	daysRemaining := int(leaf.NotAfter.Sub(now).Hours() / 24)

	issuerStr := leaf.Issuer.Organization
	if len(issuerStr) == 0 && leaf.Issuer.CommonName != "" {
		issuerStr = []string{leaf.Issuer.CommonName}
	}
	issuerName := strings.Join(issuerStr, ", ")
	if issuerName == "" {
		issuerName = leaf.Issuer.CommonName
	}

	versionStr := "TLS Unknown"
	switch state.Version {
	case tls.VersionTLS13:
		versionStr = "TLS 1.3"
	case tls.VersionTLS12:
		versionStr = "TLS 1.2"
	case tls.VersionTLS11:
		versionStr = "TLS 1.1"
	case tls.VersionTLS10:
		versionStr = "TLS 1.0"
	}

	cipherStr := tls.CipherSuiteName(state.CipherSuite)
	if cipherStr == "" {
		cipherStr = fmt.Sprintf("0x%04X", state.CipherSuite)
	}

	return &SSLCertDetails{
		CommonName:     leaf.Subject.CommonName,
		Issuer:         issuerName,
		SANs:           leaf.DNSNames,
		ValidFrom:      leaf.NotAfter.Add(-time.Duration(leaf.NotAfter.Sub(leaf.NotBefore))),
		ValidTo:        leaf.NotAfter,
		DaysRemaining:  daysRemaining,
		TLSVersion:     versionStr,
		CipherSuite:    cipherStr,
		IsExpired:      daysRemaining < 0,
		IsExpiringSoon: daysRemaining >= 0 && daysRemaining <= 14,
	}, nil
}

// InspectDNS queries DNS resolution for a domain and measures lookup latency.
func InspectDNS(ctx context.Context, rawTarget string, timeout time.Duration) *DNSDetails {
	host, _ := CleanHostPort(rawTarget, "80")
	result := &DNSDetails{
		Domain:  host,
		Success: false,
	}

	if host == "" {
		result.Error = "empty hostname"
		return result
	}

	// Create scoped context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	ips, err := net.DefaultResolver.LookupIP(queryCtx, "ip", host)
	latency := float64(time.Since(start).Microseconds()) / 1000.0
	if latency <= 0 {
		latency = 0.1
	}
	result.LookupLatencyMS = latency

	if err != nil {
		result.Error = err.Error()
		return result
	}

	var ipStrs []string
	for _, ip := range ips {
		ipStrs = append(ipStrs, ip.String())
	}
	result.IPs = ipStrs
	result.Success = true

	// Check for CNAME
	if cname, err := net.DefaultResolver.LookupCNAME(queryCtx, host); err == nil && cname != "" && cname != host+"." {
		result.CNAME = strings.TrimSuffix(cname, ".")
	}

	return result
}
