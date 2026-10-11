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
   - Sends non-blocking HTTP GET requests with custom `BotKeep-Fleet-Prober/2.0` User-Agent.
   - Measures roundtrip latency with microsecond accuracy.
   - **Live TLS/SSL Certificate Inspection**: Automatically intercepts `resp.TLS.PeerCertificates[0].NotAfter` to calculate and store the exact days remaining until SSL certificate expiration (`ssl_expiry_days`).
2. **TCP Socket Driver (`driver == "tcp"`)**:
   - Uses `net.Dialer` with a strict 4-second timeout to check direct TCP connectivity to databases, caches, and daemon ports (e.g. PostgreSQL `host:5432`, Redis `host:6379`, SSH `host:22`, Kafka `host:9092`).
   - Measures raw TCP handshake latency without requiring HTTP protocol overhead.
3. **Simulated Demo Driver (`driver == "demo"`)**:
   - Provides organic jitter for realistic local testing without external network dependencies.

### Automated Incident Webhook Alerting
When `BOTKEEP_WEBHOOK_URL` is set, the prober dispatches asynchronous non-blocking JSON webhooks upon downtime incidents (`INCIDENT_DOWN`) and recovery transitions (`INCIDENT_RESOLVED`).

---

## 5. Enterprise Integrations & Open Standards

### Prometheus / OpenMetrics Exposition (`/metrics/prometheus`)
BotKeep includes a native Prometheus scraper endpoint. Standard monitoring systems like Prometheus, Grafana Agent, or Datadog can scrape `/metrics/prometheus` directly to ingest:
- Registered targets and operational counts (`botkeep_servers_total`, `botkeep_servers_active`).
- Per-node status gauges (`botkeep_node_up`), latencies (`botkeep_node_latency_ms`), and uptime SLAs (`botkeep_node_uptime_pct`).
- SSL certificate expiration remaining days (`botkeep_node_ssl_expiry_days`).
- Real-time Go engine heap allocation (`botkeep_host_allocated_mb`), goroutines, and GC cycles.

### Fleet Configuration JSON Export (`/api/fleet/export`)
Operators can download an instantaneous backup of the entire fleet registry, drivers, endpoints, regions, and SLA history as a clean JSON document.

### Public Status Page & Incident Engine (`/status`)
BotKeep includes an integrated customer-facing status page requiring zero external hosting or third-party fees:
- **Public Component Isolation**: The `servers.is_public` boolean column allows operators to expose customer-facing endpoints while keeping internal nodes private.
- **Incident Tracking**: The `incidents` SQLite table tracks chronological post-mortems and scheduled maintenance windows with status pills (`investigating`, `identified`, `monitoring`, `resolved`).
- **Live HTMX Partial Polling**: Public visitors' browsers poll `/status/components` every 10 seconds to stream real-time latency and SLA updates without page reloads.
- **CORS-Enabled Programmatic API**: `/api/status` returns instant JSON health indicators for shields.io badges or client app integrations.

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
