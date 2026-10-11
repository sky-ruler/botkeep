package models

import (
	"database/sql"
	_ "embed"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Server struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	IPAddress    string    `json:"ip_address"`
	Region       string    `json:"region"`
	Status       string    `json:"status"` // "active", "stopped", "deploying"
	CPUUsage     float64   `json:"cpu_usage"`
	MemoryUsage  float64   `json:"memory_usage"`
	StorageUsage float64   `json:"storage_usage"`
	Uptime       string    `json:"uptime"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ClusterMetrics struct {
	AvgCPU       float64
	TotalMemory  float64
	TotalStorage float64
	ActiveCount  int
	TotalCount   int
	StoppedCount int
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

	// Single writer connection pool for SQLite robustness
	conn.SetMaxOpenConns(1)

	// Execute embedded schema
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
	db.mu.RLock()
	defer db.mu.RUnlock()

	rows, err := db.conn.Query(`
		SELECT id, name, ip_address, region, status, cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at
		FROM servers
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		var s Server
		if err := rows.Scan(
			&s.ID, &s.Name, &s.IPAddress, &s.Region, &s.Status,
			&s.CPUUsage, &s.MemoryUsage, &s.StorageUsage, &s.Uptime,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}

	return servers, rows.Err()
}

func (db *DB) GetServerByID(id string) (*Server, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var s Server
	err := db.conn.QueryRow(`
		SELECT id, name, ip_address, region, status, cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at
		FROM servers
		WHERE id = ?
	`, id).Scan(
		&s.ID, &s.Name, &s.IPAddress, &s.Region, &s.Status,
		&s.CPUUsage, &s.MemoryUsage, &s.StorageUsage, &s.Uptime,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
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

	logMsg := fmt.Sprintf("[%s] Node decommissioned and removed from cluster (%s)", name, id)
	_, _ = db.conn.Exec("INSERT INTO logs (server_id, level, message, created_at) VALUES (?, 'WARN', ?, ?)", id, logMsg, time.Now())

	return nil
}

func (db *DB) SearchServers(query, status, region string) ([]Server, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var conditions []string
	var args []any

	if query != "" {
		conditions = append(conditions, "(name LIKE ? OR id LIKE ? OR ip_address LIKE ?)")
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
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
		SELECT id, name, ip_address, region, status, cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at
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
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		var s Server
		if err := rows.Scan(
			&s.ID, &s.Name, &s.IPAddress, &s.Region, &s.Status,
			&s.CPUUsage, &s.MemoryUsage, &s.StorageUsage, &s.Uptime,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}
	return servers, rows.Err()
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

func (db *DB) UpdateServerStatus(id string, newStatus string) (*Server, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var current Server
	err := db.conn.QueryRow(`
		SELECT id, name, ip_address, region, status, cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at
		FROM servers WHERE id = ?
	`, id).Scan(
		&current.ID, &current.Name, &current.IPAddress, &current.Region, &current.Status,
		&current.CPUUsage, &current.MemoryUsage, &current.StorageUsage, &current.Uptime,
		&current.CreatedAt, &current.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}

	// Adjust metrics realistically based on status transitions
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

	// Record corresponding deployment log
	logMsg := fmt.Sprintf("[%s] State transition triggered: %s -> %s (IP: %s)", current.Name, current.Status, newStatus, current.IPAddress)
	level := "DEPLOY"
	if newStatus == "stopped" {
		level = "WARN"
	}
	_, _ = db.conn.Exec(`
		INSERT INTO logs (server_id, level, message, created_at)
		VALUES (?, ?, ?, ?)
	`, id, level, logMsg, now)

	current.Status = newStatus
	current.CPUUsage = newCPU
	current.MemoryUsage = newMem
	current.Uptime = newUptime
	current.UpdatedAt = now

	return &current, nil
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
		}, nil
	}

	var sumCPU, sumMem, sumStorage float64
	var active, stopped int

	for _, s := range servers {
		if s.Status == "active" || s.Status == "deploying" {
			active++
			sumCPU += s.CPUUsage
			sumMem += s.MemoryUsage
		} else {
			stopped++
		}
		sumStorage += s.StorageUsage
	}

	avgCPU := 0.0
	avgMem := 0.0
	avgStorage := 0.0
	if len(servers) > 0 {
		avgCPU = sumCPU / float64(len(servers))
		avgMem = sumMem / float64(len(servers))
		avgStorage = sumStorage / float64(len(servers))
	}

	// Apply subtle simulated jitter for active metrics
	if active > 0 {
		jitter := (rand.Float64() - 0.5) * 1.5
		avgCPU += jitter
		if avgCPU < 5 {
			avgCPU = 5
		}
		if avgCPU > 95 {
			avgCPU = 95
		}
	}

	return &ClusterMetrics{
		AvgCPU:       avgCPU,
		TotalMemory:  avgMem,
		TotalStorage: avgStorage,
		ActiveCount:  active,
		TotalCount:   len(servers),
		StoppedCount: stopped,
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

func (db *DB) AddLog(serverID, level, message string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.conn.Exec(`
		INSERT INTO logs (server_id, level, message, created_at)
		VALUES (?, ?, ?, ?)
	`, serverID, level, message, time.Now())
	return err
}

func (db *DB) CreateServer(name, region string) (*Server, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now()
	id := fmt.Sprintf("srv-%s-%03d", strings.ToLower(region[:2]), rand.Intn(900)+100)
	ip := fmt.Sprintf("10.0.%d.%d", rand.Intn(10)+1, rand.Intn(240)+10)
	cpu := 15.0 + rand.Float64()*30.0
	mem := 25.0 + rand.Float64()*35.0
	disk := 10.0 + rand.Float64()*20.0

	server := &Server{
		ID:           id,
		Name:         name,
		IPAddress:    ip,
		Region:       region,
		Status:       "active",
		CPUUsage:     cpu,
		MemoryUsage:  mem,
		StorageUsage: disk,
		Uptime:       "1m (provisioned)",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err := db.conn.Exec(`
		INSERT INTO servers (id, name, ip_address, region, status, cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, server.ID, server.Name, server.IPAddress, server.Region, server.Status, server.CPUUsage, server.MemoryUsage, server.StorageUsage, server.Uptime, server.CreatedAt, server.UpdatedAt)
	if err != nil {
		return nil, err
	}

	logMsg := fmt.Sprintf("[%s] Node provisioned and joined cluster: %s (IP: %s, Region: %s)", server.Name, server.ID, server.IPAddress, server.Region)
	_, _ = db.conn.Exec(`
		INSERT INTO logs (server_id, level, message, created_at)
		VALUES (?, 'DEPLOY', ?, ?)
	`, server.ID, logMsg, now)

	return server, nil
}

func (db *DB) ClearAllServers() error {
	db.mu.Lock()
	defer db.mu.Unlock()

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

func (db *DB) SeedDemoFleet() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Clear existing
	_, _ = db.conn.Exec("DELETE FROM servers")
	_, _ = db.conn.Exec("DELETE FROM logs")

	now := time.Now()

	demoServers := []Server{
		{
			ID:           "srv-ingress-01",
			Name:         "api-gateway-us1",
			IPAddress:    "10.0.1.10",
			Region:       "us-east-1",
			Status:       "active",
			CPUUsage:     42.4,
			MemoryUsage:  68.2,
			StorageUsage: 34.0,
			Uptime:       "18d 4h 12m",
			CreatedAt:    now.Add(-24 * 18 * time.Hour),
			UpdatedAt:    now,
		},
		{
			ID:           "srv-auth-master",
			Name:         "auth-service-01",
			IPAddress:    "10.0.1.15",
			Region:       "us-east-1",
			Status:       "active",
			CPUUsage:     24.8,
			MemoryUsage:  45.1,
			StorageUsage: 28.5,
			Uptime:       "24d 12h 05m",
			CreatedAt:    now.Add(-24 * 24 * time.Hour),
			UpdatedAt:    now,
		},
		{
			ID:           "srv-db-replica-eu",
			Name:         "db-replica-fra",
			IPAddress:    "10.0.2.80",
			Region:       "eu-central-1",
			Status:       "active",
			CPUUsage:     58.6,
			MemoryUsage:  82.4,
			StorageUsage: 76.5,
			Uptime:       "9d 1h 44m",
			CreatedAt:    now.Add(-24 * 9 * time.Hour),
			UpdatedAt:    now,
		},
		{
			ID:           "srv-worker-batch",
			Name:         "worker-queue-node",
			IPAddress:    "10.0.1.205",
			Region:       "us-east-1",
			Status:       "stopped",
			CPUUsage:     0.0,
			MemoryUsage:  0.0,
			StorageUsage: 15.2,
			Uptime:       "0m",
			CreatedAt:    now.Add(-24 * 5 * time.Hour),
			UpdatedAt:    now,
		},
		{
			ID:           "srv-redis-cache",
			Name:         "redis-cache-master",
			IPAddress:    "10.0.1.33",
			Region:       "us-east-1",
			Status:       "active",
			CPUUsage:     12.3,
			MemoryUsage:  39.5,
			StorageUsage: 22.0,
			Uptime:       "31d 8h 22m",
			CreatedAt:    now.Add(-24 * 31 * time.Hour),
			UpdatedAt:    now,
		},
	}

	for _, s := range demoServers {
		_, err := db.conn.Exec(`
			INSERT INTO servers (id, name, ip_address, region, status, cpu_usage, memory_usage, storage_usage, uptime, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, s.ID, s.Name, s.IPAddress, s.Region, s.Status, s.CPUUsage, s.MemoryUsage, s.StorageUsage, s.Uptime, s.CreatedAt, s.UpdatedAt)
		if err != nil {
			return err
		}
	}

	demoLogs := []struct {
		srvID string
		lvl   string
		msg   string
		t     time.Time
	}{
		{"srv-ingress-01", "INFO", "[api-gateway-us1] TLS certificate renewed successfully via Let's Encrypt", now.Add(-45 * time.Minute)},
		{"srv-auth-master", "INFO", "[auth-service-01] OAuth2 token revocation cluster synchronized across 3 regions", now.Add(-30 * time.Minute)},
		{"srv-db-replica-eu", "WARN", "[db-replica-fra] WAL replication lag reached 120ms (auto-compacting)", now.Add(-18 * time.Minute)},
		{"srv-worker-batch", "WARN", "[worker-queue-node] Gracefully drained active queue consumer workers; status set to stopped", now.Add(-12 * time.Minute)},
		{"srv-ingress-01", "INFO", "[api-gateway-us1] Rate-limiter token bucket healthy (4,210 req/s ingress)", now.Add(-5 * time.Minute)},
		{"srv-redis-cache", "INFO", "[redis-cache-master] Memory defragmentation cycle finished (hit ratio: 98.4%)", now.Add(-1 * time.Minute)},
	}

	for _, l := range demoLogs {
		_, _ = db.conn.Exec(`
			INSERT INTO logs (server_id, level, message, created_at)
			VALUES (?, ?, ?, ?)
		`, l.srvID, l.lvl, l.msg, l.t)
	}

	return nil
}
