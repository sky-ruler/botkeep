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

### 4. Telemetry & Logs Polling

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
