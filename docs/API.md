# BotKeep API & Endpoint Documentation

BotKeep follows a **Server-Driven Component Architecture** powered by [Chi](https://github.com/go-chi/chi) and [HTMX](https://htmx.org). All endpoints gracefully support standard HTTP clients while returning localized HTML partials rendered with [templ](https://templ.guide) when called with the `HX-Request: true` header.

---

## 🌐 Request Headers

| Header | Description |
| :--- | :--- |
| `HX-Request: true` | Informs BotKeep that the request originated from HTMX. The handler returns an atomic HTML partial instead of performing a full page redirect. |
| `Content-Type: application/x-www-form-urlencoded` | Standard form encoding for state mutations and node provisioning. |

---

## 📑 Endpoint Catalog

### 1. Fleet Dashboard & Web UI

#### `GET /`
- **Description**: Renders the complete application shell, including the navigation sidebar, telemetry metrics grid, fleet table, live orchestration logs terminal, and drawer container.
- **Response**: `200 OK` (Full HTML document)

#### `GET /healthz`
- **Description**: Lightweight health check endpoint for container orchestrators (Kubernetes liveness/readiness probes, Docker healthchecks).
- **Response**: `200 OK` (`text/plain: OK`)

---

### 2. Fleet Node Management

#### `GET /servers`
- **Description**: Queries and filters the fleet node catalog.
- **Query Parameters**:
  - `q` *(string, optional)*: Case-insensitive search query matching node name, ID, IPv4, or endpoint URL.
  - `status` *(string, optional)*: Filter by node status (`all`, `active`, `stopped`, `deploying`).
  - `region` *(string, optional)*: Filter by cloud region (`all`, `us-east-1`, `eu-central-1`, `ap-southeast-1`).
- **Response**:
  - `200 OK` (`text/html`): Returns `@pages.ServerTable` partial. If no nodes match, returns `@components.EmptyState`.

#### `POST /servers/new`
- **Description**: Provisions and registers a new edge node or HTTP health check probe.
- **Form Parameters**:
  - `name` *(string, required)*: Node identifier label (minimum 3 characters).
  - `region` *(string, optional)*: Cloud deployment zone (default: `us-east-1`).
  - `driver` *(string, optional)*: Prober engine driver (`http`, `system`, `demo`). Default: `demo` (or `http` if URL provided).
  - `endpoint_url` *(string, optional)*: Target URL for active HTTP probing (e.g. `https://api.github.com`).
- **Response**:
  - `200 OK` (`text/html`): Refreshed `@pages.ServerTable` partial.
  - `400 Bad Request`: Validation failure message.

#### `POST /servers/{id}/ping`
- **Description**: Triggers an **immediate on-demand health check probe** against the node, updates latency metrics and rolling SLA, and records a probe point.
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
- **Description**: Decommissions and permanently purges a node and its probe history.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier.
- **Response**:
  - `200 OK` (`text/html`): Refreshed `@pages.ServerTable` partial.

#### `GET /servers/{id}/details`
- **Description**: Returns the slide-over inspector drawer partial displaying 4 live gauges (Latency, SLA %, CPU %, RAM %), driver configuration, endpoint URL, historical probe log, and node audit trail.
- **Path Parameters**:
  - `id` *(string, required)*: Node identifier.
- **Response**:
  - `200 OK` (`text/html`): `@pages.ServerDetailDrawer` partial.

---

### 3. Telemetry & Logs Polling

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

---

### 4. Fleet Testing & Resilience Diagnostics

#### `POST /servers/seed`
- **Description**: Re-populates the database with demo fleet instances (HTTP gateways, IPC sockets, and replica clusters).
- **Response**:
  - `200 OK` (`text/html`): Refreshed `@pages.ServerTable` partial.

#### `POST /servers/clear`
- **Description**: Purges all fleet instances to test the illustrative Empty State component.
- **Response**:
  - `200 OK` (`text/html`): `@components.EmptyState` partial.

#### `POST /servers/simulate-error`
- **Description**: Intentionally triggers an HTTP 500 error response to test non-intrusive client toast notification resilience.
- **Response**:
  - `500 Internal Server Error`: Diagnostic RPC failure string.
