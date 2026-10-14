# 📖 BotKeep Explained: From Beginner to DevOps Pro

> **A plain-English, zero-jargon guide explaining every piece of BotKeep—whether you just started programming yesterday or have been managing Kubernetes clusters for 10 years.**

---

## ⚡ The 30-Second Summary

**BotKeep** is a **self-contained server monitoring dashboard**. It watches your websites, APIs, databases, and microservices 24/7, tests them to see if they are alive and fast, tracks their uptime percentage and SSL certificates, and alerts you when things break.

Most monitoring tools (like Grafana, Datadog, or New Relic) are heavy, expensive, or require installing tons of software. BotKeep is **one single file (~12 MB)**. You run it, open your browser, and you are ready.

---

## 🐣 Level 1: Explain Like I'm 5 (ELI5)

### The Doctor Analogy 🩺
Imagine you own 5 coffee shops across town. You want to know if every shop is open and serving customers.
- Instead of driving to each shop yourself every hour, you hire a **24/7 inspector** who calls each coffee shop every 15 seconds.
- If the manager answers immediately, the inspector writes down: *"Open! Took 20 milliseconds to pick up the phone."*
- If the shop is a database (like PostgreSQL or Redis), the inspector knocks on their specific back door (TCP port).
- If the shop has a health license (SSL Certificate), the inspector checks how many days before it expires so it never lapses.
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
- **Works on any screen**: Mobile phone, tablet, laptop, or 4K monitor.

---

## 📚 Level 2: Core Concepts Glossary (Short & Simple)

| Term | What it means in plain English | Why it matters |
| :--- | :--- | :--- |
| **Node / Instance** | Any website, API, or server you want to monitor (e.g. `https://mywebsite.com` or `localhost:5432`). | Each row in BotKeep represents one node. |
| **Health Probe** | An automated check (HTTP GET request or TCP socket connection) sent to see if your node is responding. | Tells you if your service is alive right now. |
| **Latency** | How long it takes (in milliseconds) for the server to respond to our check. | 25ms is blazing fast. 300ms is slow. 0ms / timeout means it is down. |
| **SLA / Uptime %** | The percentage of successful checks over recent history. | If 99 out of 100 checks succeed, your SLA is 99.0%. |
| **HTTP / HTTPS Driver** | Real web monitoring mode. BotKeep tests URLs and inspects security certificates. | Used for public websites, microservices, and web APIs. |
| **TCP Socket Driver** | Direct port connection mode (e.g. `host:5432` for Postgres, `host:6379` for Redis). | Checks databases and background services that don't speak HTTP. |
| **SSL Expiry Tracking** | BotKeep checks your HTTPS certificates and counts down the exact days until expiration. | Prevents embarrassing and costly expired security certificate outages! |
| **Prometheus Exporter** | An open-standard text endpoint (`/metrics/prometheus`) that other tools can scrape. | Lets you plug BotKeep into Grafana, Datadog, or existing enterprise tools. |
| **JSON Export** | A button that downloads all your fleet targets and history as a clean JSON backup file. | Safe, easy backups in one click. |
| **Ping Now** | A button that immediately sends a probe right that second. | Instant feedback when fixing an issue or testing a server. |
| **HTMX** | A modern web tool that lets web pages update specific pieces of the screen without refreshing the browser. | Makes BotKeep feel as fast as a desktop app without heavy JavaScript frameworks. |
| **templ** | Type-safe HTML templates written in the Go language. | Fast, reliable, and prevents broken HTML tags at compile time. |
| **SQLite WAL** | A built-in database stored in a single local file (`fleet.db`). | Zero database setup required. No MySQL or Postgres to configure. |
| **Single Binary** | The HTML, CSS, JavaScript, database engine, and Go server are compiled into one single file. | You can copy this one file to any machine and run it immediately. |
| **Synthetic Scenario** | Simulates a multi-step user journey (e.g. log in, save session cookie, fetch balance) and checks response text. | Catches broken user workflows that a simple "is the server alive" check would miss. |
| **SRE Post-Mortem** | An automatically compiled incident report calculating MTTR, 5-Whys root cause analysis, and event timelines. | Saves hours of manual incident write-ups with 1-click Markdown export for your team. |
| **Web Push & PWA** | Native browser push notifications and standalone app mode with offline shell caching. | Delivers instant alerts directly to your desktop or phone without needing Discord, Slack, or third-party bots. |

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

### Step 3: Monitor a Real Website or Database
1. In the top bar of the table, find the **Deploy** bar.
2. In the **Name** box, type: `GitHub Public API`.
3. In the **Driver** dropdown, choose: `HTTP / HTTPS`.
4. In the **Target** box, type: `https://api.github.com`.
5. Click **Deploy**.

BotKeep will immediately:
- Look up the real IP address of `api.github.com` via DNS.
- Inspect its SSL Certificate and show remaining validity (e.g. `SSL 47d`).
- Register the node into the SQLite database.
- Send automated health probes every 15 seconds.

### Step 4: Monitor a Database (TCP Port)
1. In the **Name** box, type: `primary-db`.
2. In the **Driver** dropdown, choose: `TCP Socket / DB`.
3. In the **Target** box, type: `127.0.0.1:5432` (or your database address).
4. Click **Deploy**.
5. BotKeep will directly test socket connectivity without needing HTTP!

### Step 5: Test it on Demand
- Click the **Ping** button on any row.
- Notice how only that single row updates with the exact round-trip response time (e.g. `34.2ms`) and a green status indicator—with **zero full-page reloads**!

### Step 6: Inspect Details
- Click anywhere on the row to slide open the inspector drawer from the right side.
- View rolling SLA percentage, sub-millisecond latency, TLS certificate validity, and node audit logs.

---

## 🛠️ Level 4: Senior DevOps & Systems Architecture

For engineers, systems architects, and platform leads, this section covers the internal mechanics, thread-safety model, and production deployment patterns.

### 1. Pure-Go Zero-CGO Architecture
Many Go applications that use SQLite rely on `mattn/go-sqlite3`. While popular, it requires CGO (`CGO_ENABLED=1`) and a platform-specific GCC/MinGW toolchain, which complicates cross-compiling for Linux ARM64, macOS, or Alpine Linux.

BotKeep uses `modernc.org/sqlite`, a 100% pure-Go transpilation of SQLite:
- **`CGO_ENABLED=0` Compliant**: Builds on any host OS for any target architecture without cross-compilation toolchains.
- **Self-Contained Storage**: Embeds all schema migrations directly via `//go:embed internal/models/schema.sql`.

### 2. Multi-Protocol Engine
- **HTTP / HTTPS**: Uses custom HTTP client with connection pooling, custom User-Agent, and TLS certificate extraction (`resp.TLS.PeerCertificates[0].NotAfter`).
- **TCP Socket**: Uses `net.Dialer` with a strict 4-second timeout to audit non-HTTP ports (PostgreSQL, MySQL, Redis, SSH).
- **Incident Webhook Alerting**: Asynchronously dispatches non-blocking JSON webhooks to `BOTKEEP_WEBHOOK_URL` on downtime and recovery.

### 3. OpenMetrics / Prometheus Exposition
Standard Prometheus scrapers can point directly to:
```
GET /metrics/prometheus
```
Exporting `botkeep_servers_total`, `botkeep_node_up`, `botkeep_node_latency_ms`, `botkeep_node_uptime_pct`, `botkeep_node_ssl_expiry_days`, and Go runtime memory/goroutine metrics.

### 4. 100% Responsive Design Architecture
- **Mobile Drawer**: Slide-over navigation panel (`md:hidden`) with backdrop blur.
- **Fluid Layouts**: Tailwind CSS fluid grid adapting from 1-column mobile to 2-column tablet and 4-column desktop.
- **Horizontal Scroll Protection**: Wrapped tables ensure data is readable on screens down to 360px without page breakage.

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

---

## ⚖️ Comparison: Beginner View vs. Pro View

| Concept | For the Beginner | For the Senior DevOps Engineer |
| :--- | :--- | :--- |
| **Architecture** | One small file you run like any program. | Pure-Go static binary, `CGO_ENABLED=0`, embedded static assets via `//go:embed`. |
| **Observability** | Watches websites and database ports. | Multi-protocol HTTP/HTTPS + TCP socket health checks with sub-ms timing. |
| **SSL Security** | Shows days until security certificate expires. | Live peer certificate inspection (`NotAfter`) tracking days to renewal. |
| **Integrations** | One-click JSON backup & Prometheus button. | Native Prometheus/OpenMetrics scraper endpoint + async webhook alerting. |
| **Screen Support**| Looks great on phones, iPads, and computers. | 100% responsive Tailwind layout with mobile overlay drawer. |
| **Web UI** | Webpage that updates without refreshing. | Server-Driven HTMX partial swaps rendered with type-safe `a-h/templ`. |
| **Database** | A local file created automatically. | Embedded SQLite with WAL mode, `busy_timeout=5000`, zero external daemons. |
| **RAM Usage** | Uses almost none of your laptop's memory. | Consistently under 18 MB RSS under load; minimal GC pressure. |
| **Setup Time** | Under 10 seconds. | Single step: `curl -LO binary && ./botkeep`. |

---

*BotKeep is open source under the MIT License. Contributions and feedback are welcome at [github.com/sky-ruler/botkeep](https://github.com/sky-ruler/botkeep).*
