# BotKeep Architecture & Technical Specification

> **Version**: 2.0.0  
> **Repository**: [sky-ruler/botkeep](https://github.com/sky-ruler/botkeep)  
> **Engine**: Pure-Go Single-Binary Micro-Platform

---

## 1. Core Design Philosophy

BotKeep was engineered to solve a fundamental problem in modern DevOps software: **frontend and runtime bloat**. 

Rather than bundling Node.js runtimes, Webpack asset pipelines, multi-megabyte JavaScript single-page application (SPA) bundles, or separate database servers, BotKeep compiles down to a **single, ~12 MB self-contained native binary** that runs anywhere in milliseconds with **zero external dependencies**.

```
+-----------------------------------------------------------------------------------------+
|                               BotKeep Single Executable (~12 MB)                        |
|                                                                                         |
|   +-----------------------+   +------------------------+   +------------------------+   |
|   | Chi High-Speed Router |   | Multi-Protocol Prober  |   | Type-Safe templ Engine |   |
|   |  - HTTP Handlers      |   |  - HTTP/HTTPS Probes   |   |  - Atomic Partials     |   |
|   |  - HTMX Event Swaps   |   |  - TLS Expiry Audit    |   |  - Mobile Drawer Shell |   |
|   |  - Prometheus Scraper |   |  - TCP Socket Prober   |   |  - Linear Dark Theme   |   |
|   |  - JSON Fleet Backup  |   |  - Webhook Dispatcher  |   |  - 100% Responsive     |   |
|   +-----------+-----------+   +-----------+------------+   +-----------+------------+   |
|               |                           |                            |                |
|   +-----------v---------------------------v----------------------------v------------+   |
|   |                     Pure-Go Embedded SQLite (WAL Mode)                          |   |
|   |                      modernc.org/sqlite (Zero CGO Required)                     |   |
|   +---------------------------------------------------------------------------------+   |
|   |                     Embedded Assets via Go //go:embed                           |   |
|   |                - Minified Tailwind CSS      - HTMX 2.0.4 Runtime                |   |
|   +---------------------------------------------------------------------------------+   |
+-----------------------------------------------------------------------------------------+
```

---

## 2. Single-Binary Pipeline (`//go:embed`)

All production assets required to serve the dashboard are embedded directly into the Go executable at build time using the standard library `embed` package:

```go
// assets.go
package botkeep

import "embed"

//go:embed static/*
var StaticFS embed.FS
```

In `cmd/server/main.go`, this filesystem is mounted using `fs.Sub` and served with HTTP caching headers:

```go
staticSubFS, _ := fs.Sub(botkeep.StaticFS, "static")
fileServer := http.FileServer(http.FS(staticSubFS))
r.Get("/static/*", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "public, max-age=3600")
    http.StripPrefix("/static/", fileServer).ServeHTTP(w, r)
})
```

At deployment time, operators only need to distribute the compiled executable. No `npm install`, no external CSS files, and no image folders are needed.

---

## 3. Pure-Go Embedded SQLite (`modernc.org/sqlite`)

Many Go projects using SQLite rely on `mattn/go-sqlite3`, which requires **CGO** (`CGO_ENABLED=1`) and a platform-specific GCC/MinGW toolchain. This prevents easy cross-compilation for ARM64 or other operating systems.

BotKeep uses `modernc.org/sqlite`, a 100% pure Go SQLite transpilation. This provides crucial advantages:
- **`CGO_ENABLED=0`**: Cross-compiles natively for Linux (amd64, arm64), macOS (Apple Silicon, Intel), and Windows without requiring cross-GCC toolchains.
- **WAL Mode (Write-Ahead Logging)**: Configured with high-performance concurrency pragmas:
  ```sql
  PRAGMA journal_mode = WAL;
  PRAGMA synchronous = NORMAL;
  PRAGMA foreign_keys = ON;
  PRAGMA busy_timeout = 5000;
  ```
- **Concurrency & Non-Blocking Readers**: In WAL mode, SQLite allows concurrent reader queries without blocking active writers.

---

## 4. Multi-Protocol Fleet Prober Engine (`internal/monitor`)

The Prober Engine continuously audits configured fleet nodes across multiple protocols:

```go
type Engine struct {
    db         *models.DB
    client     *http.Client
    interval   time.Duration
    webhookURL string
    mu         sync.Mutex
}
```

### Supported Protocol Drivers

1. **HTTP / HTTPS Driver (`driver == "http"`)**:
   - Sends non-blocking HTTP GET/POST/PUT/HEAD/DELETE requests with custom User-Agent.
   - Measures roundtrip latency with microsecond accuracy.
   - **Deep TLS/SSL Certificate Inspection**: Intercepts `resp.TLS.PeerCertificates` and parses NotAfter, Issuer, Subject Common Name, and SANs (Subject Alternative Names) to audit certificate expiration days.
   - **Single-Step Synthetic Assertions**: Validates expected status codes, response headers, and response body substring keyword assertions.
2. **TCP Socket Driver (`driver == "tcp"`)**:
   - Uses `net.Dialer` with a strict 4-second timeout to check direct TCP connectivity to databases, caches, and daemon ports (e.g. PostgreSQL `host:5432`, Redis `host:6379`, SSH `host:22`, Kafka `host:9092`).
   - Measures raw TCP handshake latency without requiring HTTP protocol overhead.
3. **gRPC Native Health Probing Driver (`driver == "grpc"`)**:
   - Uses direct wire-level dialer to query standard `grpc.health.v1.Health/Check` service endpoints without HTTP reverse-proxy translation.
4. **DNS Latency & Recursive Records Driver (`driver == "dns"`)**:
   - Measures DNS resolution roundtrip latency against standard and custom nameservers, recursively querying A, AAAA, CNAME, MX, and TXT records.
5. **Simulated Demo Driver (`driver == "demo"`)**:
   - Provides organic jitter and simulated outage toggles for local sandbox testing without network dependencies.

### Multi-Step Synthetic Scenarios Engine (`internal/monitor/scenario.go`)
Beyond single-ping checks, BotKeep includes a multi-step synthetic transaction runner:
- **Cookie Jar Session Persistence**: Maintains an active, in-memory `net/http/cookiejar.Jar` across sequential chained requests (e.g., Step 1: `POST /login` → Step 2: session cookie stored → Step 3: `GET /dashboard` with session cookie → Step 4: keyword assertion `Welcome, Admin`).
- **Granular Diagnostics**: Records per-step HTTP status, execution latency, and error strings.
- **In-Memory Test Flights**: Operators can execute immediate dry-run simulations (`POST /servers/{id}/scenario/test`) to validate scenario logic prior to production scheduling.

### Automated Incident Webhook Alerting
When `BOTKEEP_WEBHOOK_URL` is set, the prober dispatches asynchronous non-blocking JSON webhooks upon downtime incidents (`INCIDENT_DOWN`) and recovery transitions (`INCIDENT_RESOLVED`).

---

## 5. Enterprise Integrations & Subsystems

### Prometheus / OpenMetrics Exposition (`/metrics/prometheus`)
BotKeep includes a native Prometheus scraper endpoint. Standard monitoring systems like Prometheus, Grafana Agent, or Datadog can scrape `/metrics/prometheus` directly to ingest:
- Registered targets and operational counts (`botkeep_servers_total`, `botkeep_servers_active`).
- Per-node status gauges (`botkeep_node_up`), latencies (`botkeep_node_latency_ms`), and uptime SLAs (`botkeep_node_uptime_pct`).
- SSL certificate expiration remaining days (`botkeep_node_ssl_expiry_days`).
- Real-time Go engine heap allocation (`botkeep_host_allocated_mb`), goroutines, and GC cycles.

### Declarative GitOps Mode (`fleet.yaml`)
Operators can manage fleet monitoring topology entirely as code:
- **Bi-Directional Synchronization**: Export current configuration as clean YAML (`/api/fleet/gitops/export`) or launch with `-fleet fleet.yaml`.
- **Live File Watcher**: `internal/gitops/gitops.go` actively watches `fleet.yaml` and hot-reloads targets in the background with zero server downtime.

### Public Status Page & SRE Post-Mortem Engine (`/status`)
BotKeep includes an integrated customer-facing status page requiring zero external hosting or third-party fees:
- **Public Component Isolation**: The `servers.is_public` boolean column allows operators to expose customer-facing endpoints while keeping internal nodes private.
- **Incident Tracking**: The `incidents` SQLite table tracks chronological status updates with status pills (`investigating`, `identified`, `monitoring`, `resolved`).
- **Automated SRE Post-Mortems (`internal/models/postmortem.go`)**:
  - Automatically calculates real **MTTR** (Mean Time to Resolution).
  - Formulates a **5-Whys Root Cause Analysis** based on failure telemetry.
  - Aggregates chronological events from the audit log into a minute-by-minute timeline table.
  - Exports directly to web viewer (`/status/incidents/{id}/postmortem`) and downloadable Markdown (`/status/incidents/{id}/postmortem/download`).

### Native Browser Web Push & Progressive Web App (PWA) Engine
Direct OS desktop and mobile notifications without third-party bots, relay services, or subscription fees:
- **PWA Manifest (`static/manifest.json`)**: Configured with `standalone` display mode, dark `#09090b` theme, and fast app shortcuts.
- **Root-Scoped Service Worker (`static/sw.js`)**: Served with `Service-Worker-Allowed: /` and `Cache-Control: no-cache`. Pre-caches app shell (`botkeep-shell-v2`) for offline access.
- **W3C Web Push & Notification API (`static/js/webpush.js`)**: Handles push subscriptions, background OS notification dispatches with custom actions (*"Inspect Node"*, *"Dismiss"*), and SSE `/events` incident alerting.
- **Self-Cleaning Storage (`internal/models/webpush.go`)**: Automatically prunes expired endpoints on HTTP 410 Gone / 404 Not Found responses.

### Zero-Glue Multi-Channel Push Notification Engine (`/notifications`)
Built-in push notification dispatchers directly in the single binary, bypassing complex Alertmanager setups:
- **Native Protocol Presets**: Supports Discord (rich embed cards), Telegram (Bot API with HTML formatting), Slack (attachment blocks), ntfy.sh (free instant mobile phone push without account), and custom Webhooks.
- **Concurrent Non-Blocking Delivery**: Dispatches alerts in isolated goroutines with 5-second context timeouts, ensuring slow webhook endpoints never block health probers or UI rendering.
- **Environment Auto-Bootstrapping**: Reads standard environment variables (`BOTKEEP_DISCORD_WEBHOOK`, `BOTKEEP_TELEGRAM_BOT_TOKEN`, `BOTKEEP_NTFY_TOPIC`, etc.) and automatically provisions them into SQLite on startup.

### Autonomous Self-Healing Runbooks (`internal/monitor/healer.go`)
Transforms BotKeep from a passive monitor into an active healing engine:
- When a node fails consecutive health checks, automatically executes an HTTP remediation webhook (GitHub Actions workflow, Docker restart, Kubernetes rollout).
- **Anti-Flap Cooldown Shield**: 5-minute cooldown timer prevents repeat execution loops while containers restart.
- Records all remediation attempts, response status codes, and execution output logs in SQLite table `remediations`.

### Zero-Config Satellite Mesh & Multi-Region Consensus (`internal/monitor/satellite.go`)
Eliminates single-location false alarms caused by localized ISP routing anomalies:
- Satellites run the exact same BotKeep binary in agent mode (`botkeep agent --hub ...`).
- **Consensus Shield**: An incident is only confirmed when $\ge 2$ regions agree the target is unreachable.
- Edge latency reporting provides global round-trip latency visibility across regions (Tokyo, Frankfurt, Virginia).

---

## 6. Server-Driven UI with HTMX & `a-h/templ`

BotKeep completely rejects the complexity of client-side state hydration, Redux stores, and virtual DOM diffing:

```
User Action (Ping / Deploy) ──> HTMX POST Request ──> Chi Router Handlers
                                                            │
                                                            ▼
Client DOM Updated          <──  HTML Row Partial  <── templ Compilation
```

### Type Safety via `templ`
Every component is written in `.templ` syntax and compiled into pure Go code:
- **Zero Runtime Reflection**: HTML is generated via high-performance Go string buffers.
- **Compile-Time Type Checking**: Passing invalid structs or missing parameters results in a compile error before binary generation.
- **Targeted Swaps**: Buttons use `hx-target="#server-row-{id}"` and `hx-swap="outerHTML"`, replacing only that individual row without refreshing the rest of the page.

---

## 7. 100% Responsive Viewport Architecture

BotKeep is optimized to deliver a state-of-the-art experience across mobile phones (360px+), tablets (768px), and 4K desktop workstations:

1. **Mobile Drawer Navigation (`md:hidden`)**:
   - On screens under 768px, the desktop navigation sidebar is replaced by an overlay drawer triggered by a header hamburger menu button.
   - Includes a blurred backdrop with tap-to-dismiss behavior.
2. **Adaptive Metrics Grid**:
   - Smoothly reorganizes from a 4-column layout on wide screens to 2 columns on tablets, and single-column cards on mobile viewports.
3. **Fluid Node Inspector Drawer**:
   - Slides smoothly over from the right side (`sm:max-w-md md:max-w-lg w-full`), featuring a responsive 2x2 gauge grid on mobile.
4. **Scrollable High-Density Data Tables**:
   - The fleet table is wrapped in an overflow container (`overflow-x-auto`) to preserve complete metrics fidelity and prevent horizontal clipping on small screens.

---

## 8. Multi-Platform CI/CD Pipeline

The GitHub Actions workflow (`.github/workflows/ci.yml`) compiles and tests every commit across 6 parallel jobs:

1. **Test & Validate**: Runs `go test -v -race ./...` on Ubuntu Linux, checking for race conditions and verifying code formatting.
2. **Cross-Compilation Matrix**:
   - `linux-amd64` (Standard x86_64 servers)
   - `linux-arm64` (AWS Graviton, Raspberry Pi)
   - `darwin-arm64` (Apple Silicon M-series)
   - `darwin-amd64` (Intel macOS)
   - `windows-amd64` (Windows x86_64)
