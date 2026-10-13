CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    region TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('active', 'stopped', 'deploying', 'restarting')),
    driver TEXT NOT NULL DEFAULT 'demo' CHECK(driver IN ('http', 'tcp', 'grpc', 'dns', 'system', 'demo')),
    endpoint_url TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT 'HTTP',
    ssl_expiry_days INTEGER NOT NULL DEFAULT -1,
    ssl_issuer TEXT NOT NULL DEFAULT '',
    ssl_sans TEXT NOT NULL DEFAULT '',
    ssl_valid_to DATETIME,
    dns_latency_ms REAL NOT NULL DEFAULT 0.0,
    dns_records TEXT NOT NULL DEFAULT '',
    grpc_service TEXT NOT NULL DEFAULT '',
    http_method TEXT NOT NULL DEFAULT 'GET',
    expected_status TEXT NOT NULL DEFAULT '200',
    body_assertion TEXT NOT NULL DEFAULT '',
    custom_headers TEXT NOT NULL DEFAULT '',
    is_public INTEGER NOT NULL DEFAULT 1,
    latency_ms REAL NOT NULL DEFAULT 0.0,
    uptime_pct REAL NOT NULL DEFAULT 100.0,
    cpu_usage REAL NOT NULL DEFAULT 0.0,
    memory_usage REAL NOT NULL DEFAULT 0.0,
    storage_usage REAL NOT NULL DEFAULT 0.0,
    uptime TEXT NOT NULL DEFAULT '0m',
    last_checked DATETIME,
    runbook_url TEXT NOT NULL DEFAULT '',
    runbook_enabled INTEGER NOT NULL DEFAULT 0,
    auto_heal_attempts INTEGER NOT NULL DEFAULT 0,
    last_healed_at DATETIME,
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

CREATE TABLE IF NOT EXISTS satellites (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'online' CHECK(status IN ('online', 'offline')),
    latency_to_hub_ms REAL NOT NULL DEFAULT 0.0,
    last_heartbeat DATETIME DEFAULT CURRENT_TIMESTAMP,
    version TEXT NOT NULL DEFAULT '2.0.0',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS satellite_probes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    satellite_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    region TEXT NOT NULL,
    latency_ms REAL NOT NULL,
    status_code INTEGER NOT NULL,
    success INTEGER NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(satellite_id) REFERENCES satellites(id) ON DELETE CASCADE,
    FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS remediations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    action_name TEXT NOT NULL,
    trigger_reason TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('triggered', 'in_progress', 'healed', 'failed')),
    response_code INTEGER NOT NULL DEFAULT 0,
    output_log TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME,
    FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_servers_status ON servers(status);
CREATE INDEX IF NOT EXISTS idx_servers_driver ON servers(driver);
CREATE INDEX IF NOT EXISTS idx_probe_history_server ON probe_history(server_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_created ON incidents(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_logs_created_at ON logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_channels_enabled ON notification_channels(enabled);
CREATE INDEX IF NOT EXISTS idx_satellites_heartbeat ON satellites(last_heartbeat DESC);
CREATE INDEX IF NOT EXISTS idx_satellite_probes_server ON satellite_probes(server_id, region, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_remediations_server ON remediations(server_id, created_at DESC);
