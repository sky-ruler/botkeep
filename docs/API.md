# BotKeep API & Endpoint Documentation

BotKeep follows a **Server-Driven Component Architecture** powered by [Chi](https://github.com/go-chi/chi) and [HTMX](https://htmx.org). All endpoints gracefully support standard HTTP clients and monitoring scrapers, while returning localized HTML partials rendered with [templ](https://templ.guide) when called with the `HX-Request: true` header.

---

## 🌐 Request Headers

| Header | Description |
| :--- | :--- |
| `HX-Request: true` | Informs BotKeep that the request originated from HTMX. The handler returns an atomic HTML partial instead of performing a full page redirect. |
| `Content-Type: application/x-www-form-urlencoded` | Standard form encoding for state mutations and node provisioning. |
| `Accept: text/plain` | Used when scraping Prometheus OpenMetrics format from `/metrics/prometheus`. |
| `Accept: application/json` | Used when downloading fleet backups from `/api/fleet/export`. |

---

## 📑 Endpoint Catalog

### 1. Fleet Dashboard & Web UI

#### `GET /`
- **Description**: Renders the complete responsive application shell, including the desktop sidebar / mobile drawer overlay, telemetry metrics grid, fleet table, quick deploy bar, and live orchestration logs terminal.
- **Response**: `200 OK` (Full HTML document)

#### `GET /healthz`
- **Description**: Lightweight health check endpoint for container orchestrators (Kubernetes liveness/readiness probes, Docker healthchecks).
- **Response**: `200 OK` (`text/plain: OK`)

---

### 2. Enterprise Observability & Integrations

#### `GET /metrics/prometheus`
- **Description**: Exposes fleet telemetry, node uptime, latency, and host Go runtime statistics in standard **Prometheus / OpenMetrics** text exposition format.
- **Response**: `200 OK` (`text/plain; version=0.0.4; charset=utf-8`)
- **Exported Metric Series**:
  - `botkeep_servers_total`: Total count of registered targets in fleet.
  - `botkeep_servers_active`: Count of operational targets in fleet.
  - `botkeep_node_up{id, name, region, driver, protocol}`: Operational status of the monitored target (1 = active/up, 0 = stopped/down).
  - `botkeep_node_latency_ms{id, name}`: Sub-millisecond roundtrip probe latency.
  - `botkeep_node_uptime_pct{id, name}`: Rolling SLA compliance percentage.
  - `botkeep_node_ssl_expiry_days{id, name}`: Days remaining until peer TLS/SSL certificate expiration.
  - `botkeep_host_allocated_mb`: Heap memory allocated by the Go runtime engine in megabytes.
  - `botkeep_host_goroutines`: Count of active Go engine goroutines.
  - `botkeep_host_gc_cycles`: Total garbage collection passes performed.

#### `GET /api/fleet/export`
- **Description**: Streams the complete fleet configuration, driver definitions, target endpoints, regions, and current telemetry states as a downloadable JSON backup file (`botkeep-fleet-backup-<timestamp>.json`).
- **Response**: `200 OK` (`application/json`) with `Content-Disposition: attachment`

#### `BOTKEEP_WEBHOOK_URL` (Environment Variable)
- **Description**: When configured, BotKeep automatically dispatches asynchronous JSON alerts to your incident webhook (Slack, Discord, PagerDuty, or custom webhook) on downtime incidents and recovery events.
- **Webhook Payload Format**:
  ```json
  {
    "event": "INCIDENT_DOWN",
    "server_id": "srv-us-101",
    "server_name": "production-api-gateway",
    "region": "us-east-1",
    "driver": "http",
    "protocol": "HTTPS",
    "details": "HTTP status 503",
    "timestamp": "2026-10-12T04:20:00Z"
  }
---

### 3. Public Status Page & Incident Timelines

#### `GET /status`
- **Description**: Renders the customer-facing, public status page with real-time operational hero banners, system-wide SLA compliance percentage, public component health cards, and chronological 90-day incident/maintenance timelines.
- **Authentication**: None (Publicly accessible).
- **Response**: `200 OK` (`text/html`)

#### `GET /status/components`
- **Description**: HTMX atomic polling endpoint returning refreshed public component rows with live spark dots, latency, and operational badges (`hx-trigger="every 10s"`).
- **Response**: `200 OK` (`text/html`)

#### `GET /api/status`
- **Description**: Exposes a clean, programmatic JSON feed of current system status indicator (`operational`, `degraded`, `outage`), public components, and recent incidents (suitable for shields.io badges, custom status widgets, or integration with external apps).
- **CORS**: `Access-Control-Allow-Origin: *` enabled.
- **Response**: `200 OK` (`application/json`)
- **JSON Payload Schema**:
  ```json
  {
    "status": {
      "indicator": "operational",
      "description": "All systems operational"
    },
    "components": [...],
    "incidents": [...],
    "updated_at": "2026-10-12T04:20:00Z"
  }
  ```

#### `POST /admin/incidents/new`
- **Description**: Registers a new incident update or scheduled maintenance window.
- **Form Parameters**:
  - `title` *(string, required)*: Brief incident headline.
  - `status` *(string, required)*: `investigating`, `identified`, `monitoring`, or `resolved`.
  - `severity` *(string, required)*: `minor`, `major`, `critical`, or `maintenance`.
  - `affected_components` *(string, optional)*: Comma-separated component names.
  - `message` *(string, required)*: Markdown-formatted incident update message.
- **Response**: `303 See Other` (Redirects to `/status`)

#### `GET /status/incidents/{id}/postmortem`
- **Description**: Generates and renders a comprehensive, Linear/Vercel-styled **SRE Incident Post-Mortem** report. Calculates real Mean Time to Resolution (MTTR), formulates a 5-Whys Root Cause Analysis, compiles a chronological minute-by-minute timeline from audit logs, and lists preventative action items. Includes one-click copy to clipboard.
- **Path Parameters**:
  - `id` *(string, required)*: Incident identifier (e.g. `inc-2026-001`).
- **Response**: `200 OK` (`text/html`)

#### `GET /status/incidents/{id}/postmortem/download`
- **Description**: Generates and streams the SRE Incident Post-Mortem report directly as a downloadable standard Markdown file (`postmortem-{id}.md`) ready for ingestion into GitHub issues, Notion, or internal wikis.
- **Path Parameters**:
  - `id` *(string, required)*: Incident identifier.
- **Response**: `200 OK` (`text/markdown; charset=utf-8`) with `Content-Disposition: attachment; filename="postmortem-{id}.md"`

---

### 4. Multi-Channel Alerting & Push Notifications

#### `GET /notifications`
- **Description**: Renders the complete Notification Channels & Alerting Management UI.
- **Response**: `200 OK` (`text/html`)

#### `GET /notifications/list`
- **Description**: Returns the rendered HTMX partial containing the list of configured notification channels.
- **Response**: `200 OK` (`text/html`)

#### `POST /admin/notifications/new`
- **Description**: Provisions a new alert delivery channel with native presets.
- **Form Parameters**:
  - `name` *(string, required)*: Descriptive label (e.g. `DevOps Discord Alerts`).
  - `channel_type` *(string, required)*: Preset driver: `discord`, `telegram`, `slack`, `ntfy`, or `webhook`.
  - `webhook_url` *(string, for discord/slack/webhook)*: Webhook delivery URL.
  - `telegram_token` & `telegram_chat_id` *(string, for telegram)*: Bot token and chat ID.
  - `ntfy_topic` & `ntfy_server` *(string, for ntfy)*: ntfy topic name and server URL (`https://ntfy.sh`).
  - `events_down` & `events_recovery` *(checkbox)*: Subscribed incident transitions.
- **Response**: `200 OK` (`text/html`) with updated `@pages.ChannelsList` partial.

#### `POST /admin/notifications/{id}/toggle`
- **Description**: Toggles channel enabled/paused state without deleting configuration.
- **Response**: `200 OK` (`text/html`) with updated channels list.

#### `DELETE /admin/notifications/{id}`
- **Description**: Removes an alert channel.
- **Response**: `200 OK` (`text/html`) with updated channels list.

#### `POST /admin/notifications/{id}/test`
- **Description**: Dispatches an immediate verification test alert ping to the specified channel.
- **Response**: `200 OK` (`text/html`) with `@components.Toast` notification.

#### `POST /admin/notifications/test-all`
- **Description**: Concurrently broadcasts test alerts across all active notification channels.
- **Response**: `200 OK` (`text/html`) with `@components.Toast` notification.

#### `GET /api/notifications`
- **Description**: Programmatic JSON endpoint returning configured alert channels with masked credentials.
- **CORS**: `Access-Control-Allow-Origin: *`
- **Response**: `200 OK` (`application/json`)

#### `GET /manifest.json`
- **Description**: Web App Manifest specifying standalone PWA configuration, high-resolution icons, dark theme colors, and quick application launch shortcuts (*Fleet Overview*, *Public Status*, *Alert Channels*).
- **Response**: `200 OK` (`application/manifest+json`)

#### `GET /sw.js`
- **Description**: Root-scoped Service Worker script handling offline shell caching (`botkeep-shell-v2`), push event dispatching with system vibration patterns, notification click navigation, and client message routing.
- **Headers**: `Service-Worker-Allowed: /`, `Cache-Control: no-cache, no-store, must-revalidate`
- **Response**: `200 OK` (`application/javascript; charset=utf-8`)

#### `POST /api/push/subscribe`
- **Description**: Registers or updates an active browser Web Push subscription for direct desktop and mobile notifications.
- **Request Body** (`application/json`):
  ```json
  {
    "endpoint": "https://fcm.googleapis.com/fcm/send/...",
    "p256dh": "BNcR...",
    "auth": "tB...",
    "user_agent": "Mozilla/5.0..."
  }
  ```
- **Response**: `201 Created` (`application/json`)

#### `POST /api/push/unsubscribe`
- **Description**: Removes an active browser Web Push subscription matching the provided endpoint.
- **Request Body** (`application/json`):
  ```json
  {
    "endpoint": "https://fcm.googleapis.com/fcm/send/..."
  }
  ```
- **Response**: `200 OK` (`application/json`)

#### `POST /api/push/test`
- **Description**: Triggers an instantaneous broadcast test notification to all active browser Web Push subscribers. Automatically prunes expired subscriptions that return HTTP 410 Gone or 404 Not Found.
- **Response**: `200 OK` (`application/json`) with `{"status": "dispatched", "dispatched": 1}`

#### `GET /api/push/status`
- **Description**: Returns subscription count and list of active push subscriber devices.
- **Response**: `200 OK` (`application/json`)

#### `DELETE /api/push/subscriptions/{id}`
- **Description**: Revokes a specific push subscription by ID.
- **Path Parameters**:
  - `id` *(string, required)*: Subscription ID (e.g. `push-2a526b2170fe6d2c`).
- **Response**: `200 OK` (`application/json`)

---

### 5. Fleet Node Management & Multi-Protocol Probing

#### `GET /servers`
- **Description**: Queries and filters the fleet node catalog.
- **Query Parameters**:
  - `q` *(string, optional)*: Case-insensitive search query matching node name, ID, IPv4, or endpoint URL.
  - `status` *(string, optional)*: Filter by node status (`all`, `active`, `stopped`, `deploying`).
  - `region` *(string, optional)*: Filter by cloud region (`all`, `us-east-1`, `eu-central-1`, `ap-southeast-1`).
- **Response**:
  - `200 OK` (`text/html`): Returns `@pages.ServerTable` partial. If no nodes match, returns `@components.EmptyState`.

#### `POST /servers/new`
- **Description**: Provisions and registers a new edge node, HTTP/HTTPS web target, or TCP socket target.
- **Form Parameters**:
  - `name` *(string, required)*: Node identifier label (minimum 3 characters).
  - `region` *(string, optional)*: Cloud deployment zone (`us-east-1`, `eu-central-1`, `ap-southeast-1`). Default: `us-east-1`.
  - `driver` *(string, optional)*: Prober engine driver:
    - `http`: Web endpoint health check with automatic TLS certificate inspection.
    - `tcp`: Direct TCP socket / port health check (PostgreSQL `host:5432`, Redis `host:6379`, SSH `host:22`).
    - `demo`: Simulated node with organic telemetry jitter.
  - `endpoint_url` *(string, optional)*: Target URL or `host:port` address (e.g. `https://api.github.com` or `db.internal:5432`).
- **Response**:
  - `200 OK` (`text/html`): Refreshed `@pages.ServerTable` partial.
  - `400 Bad Request`: Validation failure message.

#### `POST /servers/{id}/ping`
- **Description**: Triggers an **immediate on-demand health check probe** against the node (HTTP GET request or TCP socket dial), updates latency and SLA, extracts TLS certificate expiry if HTTPS, and records a probe point.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier (e.g. `srv-us-101`).
- **Response**:
  - `200 OK` (`text/html`): Returns an updated `@pages.ServerRow` partial with refreshed latency and spark dots for HTMX `outerHTML` swapping.

#### `POST /servers/{id}/start`
- **Description**: Starts a stopped node, transitioning state to `active`.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier.
- **Response**:
  - `200 OK` (`text/html`): Updated `@pages.ServerRow` partial.

#### `POST /servers/{id}/stop`
- **Description**: Gracefully halts a node, transitioning state to `stopped` and zeroing CPU/memory utilization.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier.
- **Response**:
  - `200 OK` (`text/html`): Updated `@pages.ServerRow` partial.

#### `POST /servers/{id}/restart`
- **Description**: Initiates a rolling restart cycle, temporarily marking status as `restarting`.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier.
- **Response**:
  - `200 OK` (`text/html`): Updated `@pages.ServerRow` partial.

#### `DELETE /servers/{id}` *(or `POST /servers/{id}/delete`)*
- **Description**: Decommissions and permanently purges a node and its historical probe points.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier.
- **Response**:
  - `200 OK` (`text/html`): Refreshed `@pages.ServerTable` partial.

#### `GET /servers/{id}/details`
- **Description**: Returns the slide-over inspector drawer partial displaying 4 live gauges (Latency, SLA %, CPU %, RAM %), driver configuration, protocol, TLS certificate expiration days, target endpoint, historical probe log, and node audit trail.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier.
- **Response**:
  - `200 OK` (`text/html`): `@pages.ServerDetailDrawer` partial.

---

### 6. Multi-Step Synthetic Scenarios & Session Assertion Engine

#### `POST /servers/{id}/scenario`
- **Description**: Configures a multi-step synthetic transaction journey for the target node. Supports chained sequential HTTP requests (e.g. login, query, transaction) maintaining a live cookie jar session across all steps.
- **Path Parameters**:
  - `id` *(string, required)*: Target node identifier.
- **Form Parameters**:
  - `scenario_steps` *(string, required)*: JSON array of steps:
    ```json
    [
      {
        "name": "User Authentication",
        "method": "POST",
        "url": "https://api.example.com/login",
        "body": "{\"user\":\"admin\",\"pass\":\"secret\"}",
        "headers": "Content-Type: application/json",
        "expected_status": "200",
        "assertion": "token"
      },
      {
        "name": "Authenticated Query",
        "method": "GET",
        "url": "https://api.example.com/dashboard",
        "expected_status": "200",
        "assertion": "active_session"
      }
    ]
    ```
- **Response**: `200 OK` (`text/html`) with updated `@components.Toast` and refreshed scenario card.

#### `POST /servers/{id}/scenario/test`
- **Description**: Executes an immediate dry-run "Test Flight" of the configured multi-step synthetic scenario in memory, recording per-step response codes, latency breakdown, cookie persistence, and keyword assertion results without affecting production SLA.
- **Path Parameters**:
  - `id` *(string, required)*: Target node identifier.
- **Response**: `200 OK` (`text/html`) with live step-by-step diagnostic execution logs.

---

### 7. Self-Healing Runbooks & Synthetic Probing

#### `POST /servers/{id}/runbook`
- **Description**: Configures automated remediation settings for the node.
- **Path Parameters**:
  - `id` *(string, required)*: Target node identifier.
- **Form Parameters**:
  - `runbook_url` *(string)*: Target webhook URL (GitHub Action workflow dispatch, Kubernetes restart hook, Docker daemon restart).
  - `runbook_enabled` *(checkbox/string)*: `"true"` or `"1"` to enable auto-healing.
- **Response**: `200 OK` (`text/html`) with toast notification.

#### `POST /servers/{id}/runbook/trigger`
- **Description**: Manually dispatches an operator smoke test to verify connectivity and execution against the configured auto-healing runbook webhook target.
- **Path Parameters**:
  - `id` *(string, required)*: Target node identifier.
- **Response**: `200 OK` (`text/html`) with toast notification.

#### `POST /servers/{id}/synthetic`
- **Description**: Configures single-step HTTP synthetic assertion parameters.
- **Form Parameters**:
  - `http_method`: `GET`, `POST`, `PUT`, `HEAD`, `DELETE`, `PATCH`.
  - `expected_status`: Expected HTTP response status code (e.g. `200` or `201`).
  - `body_assertion`: Substring keyword that MUST be present in the response body.
  - `custom_headers`: Custom request headers formatted as `Key: Value\nKey2: Value2`.
- **Response**: `200 OK` (`text/html`) with toast notification.

---

### 8. Declarative GitOps Mode

#### `GET /api/fleet/gitops/export`
- **Description**: Streams the complete monitored fleet configuration in declarative `fleet.yaml` GitOps format, ready to be committed to version control and mounted via `-fleet fleet.yaml`.
- **Response**: `200 OK` (`application/x-yaml`) with `Content-Disposition: attachment; filename="fleet.yaml"`.

---

### 9. Zero-Config Satellite Mesh & Multi-Region Consensus

#### `GET /api/satellites`
- **Description**: Programmatic JSON feed listing all registered edge satellites, global regions, hub round-trip latency, and consensus heartbeat timestamps.
- **Response**: `200 OK` (`application/json`).

#### `POST /api/satellite/heartbeat`
- **Description**: Edge satellite heartbeat check-in endpoint.
- **Request Body** (`application/json`):
  ```json
  {
    "id": "sat-tokyo-01",
    "name": "Tokyo Edge Agent",
    "region": "ap-northeast-1",
    "ip_address": "203.0.113.19",
    "latency_to_hub_ms": 138.4,
    "version": "2.0.0"
  }
  ```
- **Response**: `200 OK` (`application/json`).

#### `POST /api/satellite/report`
- **Description**: Edge satellite regional health check reporting endpoint.
- **Request Body** (`application/json`):
  ```json
  {
    "satellite_id": "sat-tokyo-01",
    "server_id": "srv-us-101",
    "region": "ap-northeast-1",
    "latency_ms": 142.1,
    "status_code": 200,
    "success": true
  }
  ```
- **Response**: `200 OK` (`application/json`).

#### `GET /api/satellite/targets`
- **Description**: Dispatches active probe target endpoints and protocols for edge satellites to check.
- **Response**: `200 OK` (`application/json`).

---

### 10. Telemetry & Logs Polling

#### `GET /metrics`
- **Description**: Calculates and returns aggregate fleet telemetry and host runtime metrics:
  - **Fleet Capacity**: Active vs total registered node count.
  - **Avg Probe Latency**: Mean roundtrip response time across all active nodes in milliseconds.
  - **Fleet Health SLA**: Rolling uptime compliance percentage across all monitored fleet nodes.
  - **Engine Heap Memory**: Real host Go runtime allocated heap memory (MB), active goroutines, and GC cycles.
- **Polling Interval**: Triggered by client HTMX every 5 seconds (`hx-trigger="every 5s"`).
- **Response**:
  - `200 OK` (`text/html`): `@pages.MetricsGrid` partial.

#### `GET /logs`
- **Description**: Returns the rolling buffer of recent orchestration and probe audit logs.
- **Polling Interval**: Triggered by client HTMX every 4 seconds (`hx-trigger="every 4s"`).
- **Response**:
  - `200 OK` (`text/html`): `@pages.LogsStream` partial.

#### `POST /logs/clear`
- **Description**: Flushes the live telemetry log stream buffer.
- **Response**:
  - `200 OK` (`text/html`): Empty `@pages.LogsStream` partial.

#### `POST /servers/seed`
- **Description**: Re-populates the database with production demonstration fleet instances (HTTP APIs, TCP database sockets, and edge CDN endpoints with live TLS certificates).
- **Response**:
  - `200 OK` (`text/html`): Refreshed `@pages.ServerTable` partial.
