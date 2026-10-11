# 📖 BotKeep Explained: From Beginner to DevOps Pro

> **A plain-English, zero-jargon guide explaining every piece of BotKeep—whether you just started programming yesterday or have been managing Kubernetes clusters for 10 years.**

---

## ⚡ The 30-Second Summary

**BotKeep** is a **self-contained server monitoring dashboard**. It watches your websites, APIs, and servers 24/7, tests them to see if they are alive and fast, tracks their uptime percentage, and alerts you when things break.

Most monitoring tools (like Grafana, Datadog, or New Relic) are heavy, expensive, or require installing tons of software. BotKeep is **one single file (~12 MB)**. You run it, open your browser, and you are ready.

---

## 🐣 Level 1: Explain Like I'm 5 (ELI5)

### The Doctor Analogy 🩺
Imagine you own 5 coffee shops across town. You want to know if every shop is open and serving customers.
- Instead of driving to each shop yourself every hour, you hire a **24/7 inspector** who calls each coffee shop every 15 seconds.
- If the manager answers immediately, the inspector writes down: *"Open! Took 20 milliseconds to pick up the phone."*
- If nobody answers or the phone is dead, the inspector marks it as down and sounds an alarm.
- At the end of the day, you get a clean chart showing exactly when each shop was open and how fast they answered.

**BotKeep is that 24/7 inspector for your software.**

### Why does BotKeep exist?
If you try to set up traditional monitoring tools today, you often need:
- Node.js and `npm` (hundreds of megabytes of JavaScript libraries).
- A separate database server (PostgreSQL, MySQL, or InfluxDB).
- Docker, Kubernetes, or complex configuration files.
- 500 MB to 1 GB of computer memory just to display a simple webpage.

BotKeep takes the opposite path:
- **No dependencies**: Everything is packed inside a single executable file.
- **Tiny footprint**: Uses less than 18 MB of RAM.
- **Starts in 4 milliseconds**: Double-click it, and it is instantly running.

---

## 📚 Level 2: Core Concepts Glossary (Short & Simple)

| Term | What it means in plain English | Why it matters |
| :--- | :--- | :--- |
| **Node / Instance** | Any website, API, or server you want to monitor (e.g. `https://mywebsite.com` or `https://api.github.com`). | Each row in BotKeep represents one node. |
| **Health Probe** | An automated check (like a ping or HTTP request) sent to see if your node is responding properly. | Tells you if your service is working right now. |
| **Latency** | How long it takes (in milliseconds) for the server to respond to our check. | 25ms is blazing fast. 300ms is slow. 0ms / timeout means it is down. |
| **SLA / Uptime %** | The percentage of successful checks over recent history. | If 99 out of 100 checks succeed, your SLA is 99.0%. |
| **HTTP Driver** | Real network monitoring mode. BotKeep resolves the domain's real IP address via DNS and sends real HTTP requests. | This is what you use in production for your actual websites and APIs. |
| **Simulated Driver** | A demo mode that creates realistic test data so you can test the UI without needing real servers. | Great for testing and trying out BotKeep with zero setup. |
| **Ping Now** | A button that immediately sends a probe right that second, rather than waiting for the next 15-second background timer. | Instant feedback when you are fixing an issue or testing a server. |
| **HTMX** | A modern web tool that lets web pages update specific pieces of the screen without refreshing the whole browser tab. | Makes BotKeep feel as fast as a desktop app without heavy JavaScript frameworks. |
| **templ** | Type-safe HTML templates written in the Go language. | Fast, reliable, and prevents broken HTML tags at compile time. |
| **SQLite WAL** | A built-in database stored in a single local file (`fleet.db`). WAL means "Write-Ahead Logging", which allows reading and writing at the same time without database locks. | Zero database setup required. No MySQL or Postgres to configure. |
| **Single Binary** | The HTML, CSS, JavaScript, database engine, and Go server are compiled into one single file. | You can copy this one file to any machine and run it immediately. |

---

## 🚀 Level 3: Beginner Walkthrough (Your First 2 Minutes)

### Step 1: Start BotKeep
Open your terminal (PowerShell, macOS Terminal, or Linux Bash) and run:
```bash
./botkeep
```
*(On Windows, you can simply run `.\bin\botkeep.exe`)*

### Step 2: Open the Dashboard
Open your web browser and navigate to:
```
http://localhost:8080
```
You will see the dark-mode dashboard with cluster metrics, the fleet table, and streaming live logs.

### Step 3: Monitor a Real Website
1. In the top bar of the table, find the **Deploy** bar.
2. In the **Name** box, type: `GitHub Public API`.
3. In the **Driver** dropdown, choose: `Driver: HTTP Probe`.
4. In the **Target URL** box, type: `https://api.github.com`.
5. Click **Deploy**.

BotKeep will immediately:
- Look up the real IP address of `api.github.com` via DNS.
- Register the node into the SQLite database.
- Send automated health probes every 15 seconds.

### Step 4: Test it on Demand
- Find your new `GitHub Public API` row in the table.
- Click the **Ping** button.
- Notice how only that single row updates with the exact round-trip response time (e.g. `34.2ms`) and a green status indicator—with **zero full-page reloads**!

### Step 5: Inspect Details
- Click anywhere on the row.
- A slide-over panel will smoothly open from the right, showing:
  - Rolling SLA percentage.
  - Sub-millisecond latency gauge.
  - Full history of recent probe checks and HTTP status codes (e.g. `HTTP 200 OK`).
  - Audit logs for that specific node.

---

## 🛠️ Level 4: Senior DevOps & Systems Architecture

For engineers, systems architects, and platform leads, this section covers the internal mechanics, thread-safety model, and production deployment patterns.

### 1. Pure-Go Zero-CGO Architecture
Many Go applications that use SQLite rely on `mattn/go-sqlite3`. While popular, it requires CGO (`CGO_ENABLED=1`) and a platform-specific GCC/MinGW toolchain, which complicates cross-compiling for Linux ARM64, macOS, or Alpine Linux.

BotKeep uses `modernc.org/sqlite`, a 100% pure-Go transpilation of SQLite:
- **`CGO_ENABLED=0` Compliant**: Builds on any host OS for any target architecture without cross-compilation toolchains.
- **Self-Contained Storage**: Embeds all schema migrations directly via `//go:embed internal/models/schema.sql`.

### 2. Concurrency & SQLite WAL Pragmas
To handle concurrent writes from background probe workers while serving high-throughput HTMX polling requests:
```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
```
- **Write-Ahead Logging (WAL)**: Separates read and write operations into a separate `.wal` file, enabling concurrent readers while a write transaction is committed.
- **Busy Timeout**: Configured to 5,000ms to gracefully queue write transactions without throwing `database is locked` errors during bursts.
- **Connection Pool Safety**: Queries scan all result rows into memory before executing any secondary nested queries, preventing connection pool exhaustion.

### 3. Background Prober Engine (`internal/monitor`)
The monitoring engine runs as a decoupled background service:
- **Ticker Loop**: Wakes every `N` seconds (configurable via `-interval 15s`).
- **Fan-Out Workers**: Queries all active nodes and fires probes concurrently using Go goroutines and `sync.WaitGroup`.
- **Latency Measurement**: Captures network roundtrips with sub-millisecond precision:
  ```go
  start := time.Now()
  resp, err := client.Do(req)
  latencyMS := float64(time.Since(start).Microseconds()) / 1000.0
  ```
- **Rolling SLA Calculation**:
  ```go
  uptimePct := (float64(successfulProbes) / float64(totalProbes)) * 100.0
  ```

### 4. Dynamic Host Telemetry (Zero Hardcoding)
BotKeep inspects the host runtime directly without fake static placeholders:
- **Memory**: Read dynamically via `runtime.ReadMemStats(&m)` (`Alloc`, `Sys`, `NumGC`).
- **Goroutines**: Extracted via `runtime.NumGoroutine()`.
- **System Info**: Hostname retrieved via `os.Hostname()`, OS/Architecture via `runtime.GOOS/runtime.GOARCH`, and Go compiler version via `runtime.Version()`.

---

## 📦 Level 5: Production Deployment

### Option A: Minimal Linux systemd Service
Create `/etc/systemd/system/botkeep.service`:
```ini
[Unit]
Description=BotKeep DevOps Fleet Monitor
After=network.target

[Service]
Type=simple
User=botkeep
WorkingDirectory=/var/lib/botkeep
ExecStart=/usr/local/bin/botkeep -port 8080 -db /var/lib/botkeep/fleet.db -interval 15s
Restart=always
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```
Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now botkeep
```

### Option B: Ultra-Compact Docker Container (`FROM scratch`)
Because BotKeep compiles to a static, pure-Go binary with zero CGO, the container image can be built on `scratch` or `alpine`:
```dockerfile
# Build Stage
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN apk add --no-cache ca-certificates
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o botkeep ./cmd/server

# Production Image (< 18 MB total)
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/botkeep /botkeep
EXPOSE 8080
ENTRYPOINT ["/botkeep"]
```

### Option C: Hot Database Backups
Because SQLite in WAL mode safely allows live backups:
```bash
sqlite3 fleet.db ".backup /backups/fleet-$(date +%F).db"
```

---

## ⚖️ Comparison: Beginner View vs. Pro View

| Concept | For the Beginner | For the Senior DevOps Engineer |
| :--- | :--- | :--- |
| **Architecture** | One small file you run like any program. | Pure-Go static binary, `CGO_ENABLED=0`, embedded static assets via `//go:embed`. |
| **Web UI** | Webpage that updates without refreshing. | Server-Driven HTMX partial swaps rendered with type-safe `a-h/templ`. |
| **Database** | A local file created automatically. | Embedded SQLite with WAL mode, `busy_timeout=5000`, zero external daemon dependencies. |
| **Monitoring** | Checks websites every 15s and shows a ping button. | Decoupled background goroutine pool with HTTP/TLS probing, sub-ms timing, and rolling SLA. |
| **RAM Usage** | Uses almost none of your laptop's memory. | Consistently under 18 MB RSS under load; minimal GC pressure. |
| **Setup Time** | Under 10 seconds. | Single step: `curl -LO binary && ./botkeep`. |

---

*BotKeep is open source under the MIT License. Contributions and feedback are welcome at [github.com/sky-ruler/botkeep](https://github.com/sky-ruler/botkeep).*
