package models

import (
	"database/sql"
	_ "embed"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Server struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	IPAddress     string       `json:"ip_address"`
	Region        string       `json:"region"`
	Status        string       `json:"status"` // "active", "stopped", "deploying"
	Driver        string       `json:"driver"` // "http", "tcp", "system", "demo"
	EndpointURL   string       `json:"endpoint_url"`
	Protocol      string       `json:"protocol"` // "HTTP", "HTTPS", "TCP"
	SSLExpiryDays int          `json:"ssl_expiry_days"`
	LatencyMS     float64      `json:"latency_ms"`
	UptimePct     float64      `json:"uptime_pct"`
	CPUUsage      float64      `json:"cpu_usage"`
	MemoryUsage   float64      `json:"memory_usage"`
	StorageUsage  float64      `json:"storage_usage"`
	Uptime        string       `json:"uptime"`
	LastChecked   *time.Time   `json:"last_checked"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	ProbePoints   []ProbePoint `json:"probe_points,omitempty"`
}

type ProbePoint struct {
	LatencyMS  float64   `json:"latency_ms"`
	StatusCode int       `json:"status_code"`
	Success    bool      `json:"success"`
	CreatedAt  time.Time `json:"created_at"`
}

type ClusterMetrics struct {
	AvgCPU       float64
	TotalMemory  float64
	TotalStorage float64
	ActiveCount  int
	TotalCount   int
	StoppedCount int
	AvgLatencyMS float64
	AvgSLA       float64
	Host         HostMetrics
}

type HostMetrics struct {
	NumGoroutine int     `json:"num_goroutine"`
	AllocatedMB  float64 `json:"allocated_mb"`
	SysMB        float64 `json:"sys_mb"`
	NumGC        uint32  `json:"num_gc"`
	GoVersion    string  `json:"go_version"`
	OS           string  `json:"os"`
	Arch         string  `json:"arch"`
	NumCPU       int     `json:"num_cpu"`
}

type LogEntry struct {
	ID        int64     `json:"id"`
	ServerID  string    `json:"server_id"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type DB struct {
	conn *sql.DB
	mu   sync.RWMutex
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Performance Pragmas for embedded SQLite
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := conn.Exec(p); err != nil {
			return nil, fmt.Errorf("failed to execute pragma (%s): %w", p, err)
		}
	}

	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)

	// Dynamic column migrations if table existed previously from v1
	migrations := []string{
		"ALTER TABLE servers ADD COLUMN driver TEXT NOT NULL DEFAULT 'demo';",
		"ALTER TABLE servers ADD COLUMN endpoint_url TEXT NOT NULL DEFAULT '';",
		"ALTER TABLE servers ADD COLUMN latency_ms REAL NOT NULL DEFAULT 0.0;",
		"ALTER TABLE servers ADD COLUMN uptime_pct REAL NOT NULL DEFAULT 100.0;",
		"ALTER TABLE servers ADD COLUMN last_checked DATETIME;",
		"ALTER TABLE servers ADD COLUMN protocol TEXT NOT NULL DEFAULT 'HTTP';",
		"ALTER TABLE servers ADD COLUMN ssl_expiry_days INTEGER NOT NULL DEFAULT -1;",
	}
	for _, m := range migrations {
		_, _ = conn.Exec(m)
	}

	// Execute embedded schema (tables, foreign keys, indexes)
	if _, err := conn.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("failed to execute embedded schema: %w", err)
	}

	db := &DB{conn: conn}

	// Auto-seed if table is empty
	count, err := db.CountServers()
	if err == nil && count == 0 {
		_ = db.SeedDemoFleet()
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) CountServers() (int, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM servers").Scan(&count)
	return count, err
}

func (db *DB) GetAllServers() ([]Server, error) {
	return db.SearchServers("", "all", "all")
}

func (db *DB) GetServerByID(id string) (*Server, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var s Server
	var lastChecked sql.NullTime
	err := db.conn.QueryRow(`
		SELECT id, name, ip_address, region, status, driver, endpoint_url, protocol, ssl_expiry_days, latency_ms, uptime_pct,
		       cpu_usage, memory_usage, storage_usage, uptime, last_checked, created_at, updated_at
		FROM servers
		WHERE id = ?
	`, id).Scan(
		&s.ID, &s.Name, &s.IPAddress, &s.Region, &s.Status, &s.Driver, &s.EndpointURL, &s.Protocol, &s.SSLExpiryDays, &s.LatencyMS, &s.UptimePct,
		&s.CPUUsage, &s.MemoryUsage, &s.StorageUsage, &s.Uptime, &lastChecked, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if lastChecked.Valid {
		s.LastChecked = &lastChecked.Time
	}

	// Attach recent probe history for sparklines
	s.ProbePoints, _ = db.getProbeHistoryUnsafe(s.ID, 20)

	return &s, nil
}

func (db *DB) DeleteServer(id string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	var name string
	_ = db.conn.QueryRow("SELECT name FROM servers WHERE id = ?", id).Scan(&name)

	_, err := db.conn.Exec("DELETE FROM servers WHERE id = ?", id)
	if err != nil {
		return err
	}

	logMsg := fmt.Sprintf("[%s] Node decommissioned and removed from fleet (%s)", name, id)
	_, _ = db.conn.Exec("INSERT INTO logs (server_id, level, message, created_at) VALUES (?, 'WARN', ?, ?)", id, logMsg, time.Now())

	return nil
}

func (db *DB) SearchServers(query, status, region string) ([]Server, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var conditions []string
	var args []any

	if query != "" {
		conditions = append(conditions, "(name LIKE ? OR id LIKE ? OR ip_address LIKE ? OR endpoint_url LIKE ?)")
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}

	if status != "" && status != "all" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}

	if region != "" && region != "all" {
		conditions = append(conditions, "region = ?")
		args = append(args, region)
	}

	sqlQuery := `
		SELECT id, name, ip_address, region, status, driver, endpoint_url, protocol, ssl_expiry_days, latency_ms, uptime_pct,
		       cpu_usage, memory_usage, storage_usage, uptime, last_checked, created_at, updated_at
		FROM servers
	`
	if len(conditions) > 0 {
		sqlQuery += " WHERE " + strings.Join(conditions, " AND ")
	}
	sqlQuery += " ORDER BY created_at ASC"

	rows, err := db.conn.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}

	var servers []Server
	for rows.Next() {
		var s Server
		var lastChecked sql.NullTime
		if err := rows.Scan(
			&s.ID, &s.Name, &s.IPAddress, &s.Region, &s.Status, &s.Driver, &s.EndpointURL, &s.Protocol, &s.SSLExpiryDays, &s.LatencyMS, &s.UptimePct,
			&s.CPUUsage, &s.MemoryUsage, &s.StorageUsage, &s.Uptime, &lastChecked, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if lastChecked.Valid {
			s.LastChecked = &lastChecked.Time
		}
		servers = append(servers, s)
	}
	_ = rows.Close()

	// Populate probe points safely after releasing the rows connection
	for i := range servers {
		servers[i].ProbePoints, _ = db.getProbeHistoryUnsafe(servers[i].ID, 12)
	}

	return servers, nil
}

func (db *DB) getProbeHistoryUnsafe(serverID string, limit int) ([]ProbePoint, error) {
	rows, err := db.conn.Query(`
		SELECT latency_ms, status_code, success, created_at
		FROM probe_history
		WHERE server_id = ?
		ORDER BY id DESC
		LIMIT ?
	`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []ProbePoint
	for rows.Next() {
		var p ProbePoint
		var successInt int
		if err := rows.Scan(&p.LatencyMS, &p.StatusCode, &successInt, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Success = successInt == 1
		points = append(points, p)
	}
	// Reverse to chronological order (oldest to newest)
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
	return points, nil
}

func (db *DB) RecordProbe(serverID string, latencyMS float64, statusCode int, success bool, errMsg string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now()
	successInt := 0
	newStatus := "stopped"
	if success {
		successInt = 1
		newStatus = "active"
	}

	// 1. Insert history point
	_, err := db.conn.Exec(`
		INSERT INTO probe_history (server_id, latency_ms, status_code, success, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, serverID, latencyMS, statusCode, successInt, now)
	if err != nil {
		return err
	}

	// 2. Fetch current status and calculate rolling uptime
	var current Server
	err = db.conn.QueryRow(`SELECT id, name, status, cpu_usage, memory_usage FROM servers WHERE id = ?`, serverID).
		Scan(&current.ID, &current.Name, &current.Status, &current.CPUUsage, &current.MemoryUsage)
	if err != nil {
		return err
	}

	// Calculate uptime pct from last 30 probes
	var totalProbes, successProbes int
	_ = db.conn.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(success), 0)
		FROM (SELECT success FROM probe_history WHERE server_id = ? ORDER BY id DESC LIMIT 30)
	`, serverID).Scan(&totalProbes, &successProbes)

	uptimePct := 100.0
	if totalProbes > 0 {
		uptimePct = (float64(successProbes) / float64(totalProbes)) * 100.0
	}

	// Adjust metrics realistically
	cpu := current.CPUUsage
	mem := current.MemoryUsage
	if success {
		cpu = 15.0 + (latencyMS / 20.0)
		if cpu > 85 {
			cpu = 85
		}
		mem = 40.0 + rand.Float64()*10.0
	} else {
		cpu = 0.0
		mem = 0.0
	}

	// 3. Update server record
	_, err = db.conn.Exec(`
		UPDATE servers
		SET status = ?, latency_ms = ?, uptime_pct = ?, cpu_usage = ?, memory_usage = ?, last_checked = ?, updated_at = ?
		WHERE id = ?
	`, newStatus, latencyMS, uptimePct, cpu, mem, now, now, serverID)
	if err != nil {
		return err
	}

	// 4. Log alert if state changed or failure occurred
	if current.Status != newStatus || !success {
		level := "DEPLOY"
		msg := fmt.Sprintf("[%s] Health probe OK: %.1fms (HTTP %d)", current.Name, latencyMS, statusCode)
		if !success {
			level = "ERROR"
			msg = fmt.Sprintf("[%s] Health probe FAILURE: %s (Status: %d, %.1fms)", current.Name, errMsg, statusCode, latencyMS)
		}
		_, _ = db.conn.Exec(`INSERT INTO logs (server_id, level, message, created_at) VALUES (?, ?, ?, ?)`, serverID, level, msg, now)
	}

	return nil
}

func (db *DB) UpdateServerStatus(id string, newStatus string) (*Server, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var current Server
	var lastChecked sql.NullTime
	err := db.conn.QueryRow(`
		SELECT id, name, ip_address, region, status, driver, endpoint_url, latency_ms, uptime_pct,
		       cpu_usage, memory_usage, storage_usage, uptime, last_checked, created_at, updated_at
		FROM servers WHERE id = ?
	`, id).Scan(
		&current.ID, &current.Name, &current.IPAddress, &current.Region, &current.Status, &current.Driver, &current.EndpointURL,
		&current.LatencyMS, &current.UptimePct, &current.CPUUsage, &current.MemoryUsage, &current.StorageUsage, &current.Uptime,
		&lastChecked, &current.CreatedAt, &current.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}

	var newCPU, newMem float64
	var newUptime string

	switch newStatus {
	case "active":
		newCPU = 18.0 + rand.Float64()*35.0
		newMem = 35.0 + rand.Float64()*40.0
		newUptime = "1m (just started)"
	case "stopped":
		newCPU = 0.0
		newMem = 0.0
		newUptime = "0m"
	case "deploying", "restarting":
		newCPU = 65.0 + rand.Float64()*25.0
		newMem = 55.0 + rand.Float64()*20.0
		newUptime = "initializing..."
	default:
		newCPU = current.CPUUsage
		newMem = current.MemoryUsage
		newUptime = current.Uptime
	}

	now := time.Now()
	_, err = db.conn.Exec(`
		UPDATE servers
		SET status = ?, cpu_usage = ?, memory_usage = ?, uptime = ?, updated_at = ?
		WHERE id = ?
	`, newStatus, newCPU, newMem, newUptime, now, id)
	if err != nil {
		return nil, err
	}

	logMsg := fmt.Sprintf("[%s] State transition triggered: %s -> %s (Driver: %s)", current.Name, current.Status, newStatus, current.Driver)
	level := "DEPLOY"
	if newStatus == "stopped" {
		level = "WARN"
	}
	_, _ = db.conn.Exec(`INSERT INTO logs (server_id, level, message, created_at) VALUES (?, ?, ?, ?)`, id, level, logMsg, now)

	current.Status = newStatus
	current.CPUUsage = newCPU
	current.MemoryUsage = newMem
	current.Uptime = newUptime
	current.UpdatedAt = now

	return &current, nil
}

func (db *DB) CreateServer(name, region, driver, endpointURL string) (*Server, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now()
	id := fmt.Sprintf("srv-%s-%03d", strings.ToLower(region[:2]), rand.Intn(900)+100)
	ip := fmt.Sprintf("10.0.%d.%d", rand.Intn(10)+1, rand.Intn(240)+10)
	latency := 0.0
	protocol := "HTTP"
	sslExpiryDays := -1

	// Resolve real DNS IP if an HTTP/HTTPS or TCP endpoint URL is provided
	if driver == "http" && endpointURL != "" {
		if strings.HasPrefix(strings.ToLower(endpointURL), "https://") {
			protocol = "HTTPS"
		}
		if u, err := url.Parse(endpointURL); err == nil {
			host := u.Hostname()
			if host != "" {
				if ips, err := net.LookupIP(host); err == nil && len(ips) > 0 {
					ip = ips[0].String()
				} else {
					ip = host
				}
			}
		}
	} else if driver == "tcp" && endpointURL != "" {
		protocol = "TCP"
		host, _, err := net.SplitHostPort(endpointURL)
		if err == nil && host != "" {
			if ips, err := net.LookupIP(host); err == nil && len(ips) > 0 {
				ip = ips[0].String()
			} else {
				ip = host
			}
		} else {
			ip = endpointURL
		}
	} else if driver == "demo" {
		latency = 28.5 + rand.Float64()*15.0
	}

	cpu := 15.0 + rand.Float64()*30.0
	mem := 25.0 + rand.Float64()*35.0
	disk := 10.0 + rand.Float64()*20.0

	server := &Server{
		ID:            id,
		Name:          name,
		IPAddress:     ip,
		Region:        region,
		Status:        "active",
		Driver:        driver,
		EndpointURL:   endpointURL,
		Protocol:      protocol,
		SSLExpiryDays: sslExpiryDays,
		LatencyMS:     latency,
		UptimePct:     100.0,
		CPUUsage:      cpu,
		MemoryUsage:   mem,
		StorageUsage:  disk,
		Uptime:        "1m (provisioned)",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	_, err := db.conn.Exec(`
		INSERT INTO servers (id, name, ip_address, region, status, driver, endpoint_url, protocol, ssl_expiry_days, latency_ms, uptime_pct,
		                     cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, server.ID, server.Name, server.IPAddress, server.Region, server.Status, server.Driver, server.EndpointURL,
		server.Protocol, server.SSLExpiryDays, server.LatencyMS, server.UptimePct, server.CPUUsage, server.MemoryUsage,
		server.StorageUsage, server.Uptime, server.CreatedAt, server.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Seed initial probe point only for simulated demo nodes
	if driver == "demo" {
		_, _ = db.conn.Exec(`
			INSERT INTO probe_history (server_id, latency_ms, status_code, success, created_at)
			VALUES (?, ?, 200, 1, ?)
		`, server.ID, latency, now)
	}

	logMsg := fmt.Sprintf("[%s] Node provisioned (%s, Driver: %s, Protocol: %s, Endpoint: %s)", server.Name, server.ID, server.Driver, server.Protocol, server.EndpointURL)
	_, _ = db.conn.Exec(`INSERT INTO logs (server_id, level, message, created_at) VALUES (?, 'DEPLOY', ?, ?)`, server.ID, logMsg, now)

	return server, nil
}

func (db *DB) UpdateSSLExpiry(serverID string, days int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, err := db.conn.Exec("UPDATE servers SET ssl_expiry_days = ? WHERE id = ?", days, serverID)
	return err
}

func (db *DB) ExportFleet() ([]Server, error) {
	return db.GetAllServers()
}

func (db *DB) ClearAllServers() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, _ = db.conn.Exec("DELETE FROM probe_history")
	_, err := db.conn.Exec("DELETE FROM servers")
	if err != nil {
		return err
	}

	_, _ = db.conn.Exec(`
		INSERT INTO logs (server_id, level, message, created_at)
		VALUES ('system', 'WARN', 'Cluster decommissioned: all active fleet nodes purged.', ?)
	`, time.Now())

	return nil
}

func (db *DB) GetMetrics() (*ClusterMetrics, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	servers, err := db.GetAllServers()
	if err != nil {
		return nil, err
	}

	if len(servers) == 0 {
		return &ClusterMetrics{
			AvgCPU:       0,
			TotalMemory:  0,
			TotalStorage: 0,
			ActiveCount:  0,
			TotalCount:   0,
			StoppedCount: 0,
			AvgLatencyMS: 0,
			AvgSLA:       100.0,
			Host:         GetHostMetrics(),
		}, nil
	}

	var sumCPU, sumMem, sumStorage, sumLatency, sumSLA float64
	var active, stopped int

	for _, s := range servers {
		if s.Status == "active" || s.Status == "deploying" {
			active++
			sumCPU += s.CPUUsage
			sumMem += s.MemoryUsage
			sumLatency += s.LatencyMS
		} else {
			stopped++
		}
		sumStorage += s.StorageUsage
		sumSLA += s.UptimePct
	}

	avgCPU := sumCPU / float64(len(servers))
	avgMem := sumMem / float64(len(servers))
	avgStorage := sumStorage / float64(len(servers))
	avgSLA := sumSLA / float64(len(servers))
	avgLatency := 0.0
	if active > 0 {
		avgLatency = sumLatency / float64(active)
	}

	return &ClusterMetrics{
		AvgCPU:       avgCPU,
		TotalMemory:  avgMem,
		TotalStorage: avgStorage,
		ActiveCount:  active,
		TotalCount:   len(servers),
		StoppedCount: stopped,
		AvgLatencyMS: avgLatency,
		AvgSLA:       avgSLA,
		Host:         GetHostMetrics(),
	}, nil
}

func (db *DB) GetRecentLogs(limit int) ([]LogEntry, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}

	rows, err := db.conn.Query(`
		SELECT id, COALESCE(server_id, ''), level, message, created_at
		FROM logs
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.ID, &l.ServerID, &l.Level, &l.Message, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}

	return logs, rows.Err()
}

func (db *DB) GetServerLogs(serverID string, limit int) ([]LogEntry, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	rows, err := db.conn.Query(`
		SELECT id, COALESCE(server_id, ''), level, message, created_at
		FROM logs
		WHERE server_id = ?
		ORDER BY id DESC
		LIMIT ?
	`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.ID, &l.ServerID, &l.Level, &l.Message, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (db *DB) AddLog(serverID, level, message string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.conn.Exec(`
		INSERT INTO logs (server_id, level, message, created_at)
		VALUES (?, ?, ?, ?)
	`, serverID, level, message, time.Now())
	return err
}

func (db *DB) SeedDemoFleet() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, _ = db.conn.Exec("DELETE FROM probe_history")
	_, _ = db.conn.Exec("DELETE FROM servers")
	_, _ = db.conn.Exec("DELETE FROM logs")

	now := time.Now()

	demoServers := []Server{
		{
			ID:            "srv-us-101",
			Name:          "github-api-gateway",
			IPAddress:     "140.82.112.4",
			Region:        "us-east-1",
			Status:        "active",
			Driver:        "http",
			EndpointURL:   "https://api.github.com",
			Protocol:      "HTTPS",
			SSLExpiryDays: 84,
			LatencyMS:     36.4,
			UptimePct:     99.98,
			CPUUsage:      24.5,
			MemoryUsage:   48.2,
			StorageUsage:  34.0,
			Uptime:        "18d 4h",
			CreatedAt:     now.Add(-24 * 18 * time.Hour),
			UpdatedAt:     now,
		},
		{
			ID:            "srv-us-102",
			Name:          "postgres-cluster-pri",
			IPAddress:     "10.0.1.15",
			Region:        "us-east-1",
			Status:        "active",
			Driver:        "tcp",
			EndpointURL:   "db.us-east-1.internal:5432",
			Protocol:      "TCP",
			SSLExpiryDays: -1,
			LatencyMS:     2.8,
			UptimePct:     100.0,
			CPUUsage:      18.2,
			MemoryUsage:   38.5,
			StorageUsage:  28.5,
			Uptime:        "24d 12h",
			CreatedAt:     now.Add(-24 * 24 * time.Hour),
			UpdatedAt:     now,
		},
		{
			ID:            "srv-eu-201",
			Name:          "redis-cache-fra",
			IPAddress:     "10.0.2.80",
			Region:        "eu-central-1",
			Status:        "active",
			Driver:        "tcp",
			EndpointURL:   "cache.eu-central-1.internal:6379",
			Protocol:      "TCP",
			SSLExpiryDays: -1,
			LatencyMS:     1.4,
			UptimePct:     99.95,
			CPUUsage:      12.6,
			MemoryUsage:   42.4,
			StorageUsage:  16.5,
			Uptime:        "9d 1h",
			CreatedAt:     now.Add(-24 * 9 * time.Hour),
			UpdatedAt:     now,
		},
		{
			ID:            "srv-us-205",
			Name:          "worker-batch-node",
			IPAddress:     "10.0.1.205",
			Region:        "us-east-1",
			Status:        "stopped",
			Driver:        "demo",
			EndpointURL:   "service:worker-queue",
			Protocol:      "HTTP",
			SSLExpiryDays: -1,
			LatencyMS:     0.0,
			UptimePct:     94.50,
			CPUUsage:      0.0,
			MemoryUsage:   0.0,
			StorageUsage:  15.2,
			Uptime:        "0m",
			CreatedAt:     now.Add(-24 * 5 * time.Hour),
			UpdatedAt:     now,
		},
		{
			ID:            "srv-ap-301",
			Name:          "cloudflare-cdn-edge",
			IPAddress:     "104.16.124.96",
			Region:        "ap-southeast-1",
			Status:        "active",
			Driver:        "http",
			EndpointURL:   "https://cloudflare.com",
			Protocol:      "HTTPS",
			SSLExpiryDays: 142,
			LatencyMS:     18.4,
			UptimePct:     99.99,
			CPUUsage:      22.3,
			MemoryUsage:   41.5,
			StorageUsage:  22.0,
			Uptime:        "31d 8h",
			CreatedAt:     now.Add(-24 * 31 * time.Hour),
			UpdatedAt:     now,
		},
	}

	for _, s := range demoServers {
		_, err := db.conn.Exec(`
			INSERT INTO servers (id, name, ip_address, region, status, driver, endpoint_url, protocol, ssl_expiry_days, latency_ms, uptime_pct,
			                     cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, s.ID, s.Name, s.IPAddress, s.Region, s.Status, s.Driver, s.EndpointURL, s.Protocol, s.SSLExpiryDays, s.LatencyMS, s.UptimePct,
			s.CPUUsage, s.MemoryUsage, s.StorageUsage, s.Uptime, s.CreatedAt, s.UpdatedAt)
		if err != nil {
			return err
		}

		// Seed 12 probe points per server for sparkbars
		for i := 11; i >= 0; i-- {
			success := 1
			lat := s.LatencyMS + (rand.Float64()-0.5)*10.0
			code := 200
			if s.Status == "stopped" && i < 3 {
				success = 0
				code = 503
				lat = 0
			}
			_, _ = db.conn.Exec(`
				INSERT INTO probe_history (server_id, latency_ms, status_code, success, created_at)
				VALUES (?, ?, ?, ?, ?)
			`, s.ID, lat, code, success, now.Add(-time.Duration(i*15)*time.Minute))
		}
	}

	demoLogs := []struct {
		srvID string
		lvl   string
		msg   string
		t     time.Time
	}{
		{"srv-us-101", "INFO", "[github-api-gateway] Automated probe ping OK (HTTP 200, latency 42.4ms)", now.Add(-45 * time.Minute)},
		{"srv-us-102", "INFO", "[auth-cluster-master] IPC heartbeat verified over /var/run/auth-daemon.sock", now.Add(-30 * time.Minute)},
		{"srv-eu-201", "WARN", "[db-replica-fra] WAL replication lag reached 120ms (auto-compacting)", now.Add(-18 * time.Minute)},
		{"srv-us-205", "WARN", "[worker-batch-node] Service paused by operator; status set to stopped", now.Add(-12 * time.Minute)},
		{"srv-us-101", "DEPLOY", "[github-api-gateway] Health probe interval set to 15s via HTTP prober driver", now.Add(-5 * time.Minute)},
	}

	for _, l := range demoLogs {
		_, _ = db.conn.Exec(`INSERT INTO logs (server_id, level, message, created_at) VALUES (?, ?, ?, ?)`, l.srvID, l.lvl, l.msg, l.t)
	}

	return nil
}

func GetHostMetrics() HostMetrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return HostMetrics{
		NumGoroutine: runtime.NumGoroutine(),
		AllocatedMB:  float64(m.Alloc) / 1024.0 / 1024.0,
		SysMB:        float64(m.Sys) / 1024.0 / 1024.0,
		NumGC:        m.NumGC,
		GoVersion:    runtime.Version(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
	}
}
