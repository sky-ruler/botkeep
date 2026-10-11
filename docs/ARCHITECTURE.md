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
|   | Chi High-Speed Router |   | Active Prober Engine   |   | Type-Safe templ Engine |   |
|   |  - HTTP Handlers      |   |  - HTTP/TLS Handshake  |   |  - Atomic Partials     |   |
|   |  - HTMX Event Swaps   |   |  - Sub-ms Latency SLA  |   |  - Linear Dark Shell   |   |
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

## 4. Background Fleet Prober Engine (`internal/monitor`)

The Prober Engine continuously audits configured fleet nodes and HTTP endpoints:

```go
type Engine struct {
    db       *models.DB
    client   *http.Client
    interval time.Duration
    mu       sync.Mutex
}
```

### Worker Model
1. **Sweep Loop**: Runs on a configurable ticker (default: 15 seconds, controlled by `-interval` flag).
2. **Concurrent Goroutines**: Spawns concurrent probe workers across nodes with `sync.WaitGroup` to avoid serialization delays.
3. **HTTP Probe Execution**:
   - Sends non-blocking HTTP requests with a strict 5-second timeout and custom User-Agent.
   - Measures round-trip latency with microsecond accuracy:
     ```go
     latency := float64(time.Since(start).Microseconds()) / 1000.0
     ```
   - Records latency, status code, success state, and timestamp into `probe_history`.
4. **Rolling SLA Calculation**:
   - Automatically computes rolling uptime SLA across the recent probe history window:
     $$\text{SLA} = \left(\frac{\text{Successful Probes}}{\text{Total Probes}}\right) \times 100\%$$
   - State transition alerts are pushed directly to the `logs` table.
5. **Instant On-Demand Probe ("Ping Now")**:
   - The `POST /servers/{id}/ping` endpoint calls `engine.ProbeNow(ctx, id)` to execute an immediate probe out of band, returning an updated row partial to HTMX.

---

## 5. Server-Driven UI with HTMX & `a-h/templ`

BotKeep completely rejects the complexity of client-side state hydration, Redux stores, and virtual DOM diffing:

```
User Click ("Ping") ──> HTMX POST Request ──> Chi Router Handlers
                                                    │
                                                    ▼
Client DOM Updated  <──  HTML Row Partial  <── templ Compilation
```

### Type Safety via `templ`
Every component is written in `.templ` syntax and compiled into pure Go code:
- **Zero Runtime Reflection**: HTML is generated via high-performance Go string buffers.
- **Compile-Time Type Checking**: Passing invalid structs or missing parameters results in a compile error before binary generation.
- **Targeted Swaps**: Buttons use `hx-target="#server-row-{id}"` and `hx-swap="outerHTML"`, so only that individual table row is replaced.

---

## 6. Linear / Vercel Dark Mode Design System

BotKeep uses a curated dark mode palette tailored for DevOps dashboards:

| Token | Class | Description |
| :--- | :--- | :--- |
| **Canvas** | `bg-zinc-950` | Primary background canvas |
| **Surface** | `bg-zinc-900/60` | Card and drawer surfaces |
| **Borders** | `border-zinc-800` | Subtle hairline dividers |
| **Active Accent** | `text-emerald-400` / `bg-emerald-500/10` | Running instances & healthy SLA |
| **HTTP Accent** | `text-cyan-400` / `bg-cyan-500/10` | HTTP probe driver & latency metrics |
| **System Accent** | `text-purple-400` / `bg-purple-500/10` | IPC and daemon sockets |
| **Warning/Error** | `text-rose-400` / `bg-rose-500/10` | Outages and connection failures |

---

## 7. Multi-Platform CI/CD Pipeline

The GitHub Actions workflow (`.github/workflows/ci.yml`) compiles and tests every commit across 6 parallel jobs:

1. **Test & Validate**: Runs `go test -v -race ./...` on Ubuntu Linux, checking for race conditions and verifying code formatting.
2. **Cross-Compilation Matrix**:
   - `linux-amd64` (Standard x86_64 servers)
   - `linux-arm64` (AWS Graviton, Raspberry Pi)
   - `darwin-arm64` (Apple Silicon M-series)
   - `darwin-amd64` (Intel macOS)
   - `windows-amd64` (Windows x86_64)

Each artifact is compiled with `-ldflags "-s -w"` to strip debug symbols, resulting in lightweight, distribution-ready single binaries.
