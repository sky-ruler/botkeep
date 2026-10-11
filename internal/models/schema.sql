CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    region TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('active', 'stopped', 'deploying', 'restarting')),
    cpu_usage REAL NOT NULL DEFAULT 0.0,
    memory_usage REAL NOT NULL DEFAULT 0.0,
    storage_usage REAL NOT NULL DEFAULT 0.0,
    uptime TEXT NOT NULL DEFAULT '0m',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT,
    level TEXT NOT NULL CHECK(level IN ('INFO', 'WARN', 'ERROR', 'DEPLOY')),
    message TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_servers_status ON servers(status);
CREATE INDEX IF NOT EXISTS idx_logs_created_at ON logs(created_at DESC);
