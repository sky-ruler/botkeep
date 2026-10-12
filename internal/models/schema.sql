CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    region TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('active', 'stopped', 'deploying', 'restarting')),
    driver TEXT NOT NULL DEFAULT 'demo' CHECK(driver IN ('http', 'tcp', 'system', 'demo')),
    endpoint_url TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT 'HTTP',
    ssl_expiry_days INTEGER NOT NULL DEFAULT -1,
    is_public INTEGER NOT NULL DEFAULT 1,
    latency_ms REAL NOT NULL DEFAULT 0.0,
    uptime_pct REAL NOT NULL DEFAULT 100.0,
    cpu_usage REAL NOT NULL DEFAULT 0.0,
    memory_usage REAL NOT NULL DEFAULT 0.0,
    storage_usage REAL NOT NULL DEFAULT 0.0,
    uptime TEXT NOT NULL DEFAULT '0m',
    last_checked DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS probe_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    latency_ms REAL NOT NULL,
    status_code INTEGER NOT NULL,
    success INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS incidents (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('investigating', 'identified', 'monitoring', 'resolved')),
    severity TEXT NOT NULL CHECK(severity IN ('minor', 'major', 'critical', 'maintenance')),
    affected_components TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME
);

CREATE TABLE IF NOT EXISTS logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT,
    level TEXT NOT NULL CHECK(level IN ('INFO', 'WARN', 'ERROR', 'DEPLOY')),
    message TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notification_channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    channel_type TEXT NOT NULL CHECK(channel_type IN ('discord', 'slack', 'telegram', 'ntfy', 'webhook')),
    config_json TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    events TEXT NOT NULL DEFAULT 'down,recovery',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_dispatched_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_servers_status ON servers(status);
CREATE INDEX IF NOT EXISTS idx_servers_driver ON servers(driver);
CREATE INDEX IF NOT EXISTS idx_probe_history_server ON probe_history(server_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_created ON incidents(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_logs_created_at ON logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_channels_enabled ON notification_channels(enabled);
