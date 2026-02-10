# Deskmon Agent API Contract

What the macOS app expects from `deskmon-agent`. This is the source of truth for the JSON shapes the Swift client will decode.

**Default port:** `7654`
**Auth:** `Authorization: Bearer <token>` header (required on all endpoints except `/health`)

---

## Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/health` | No | Reachability check |
| `GET` | `/stats` | Yes | Full system + container stats |
| `GET` | `/stats/system` | Yes | System stats only |
| `GET` | `/stats/docker` | Yes | Docker container stats only |
| `GET` | `/stats/processes` | Yes | Top processes by CPU |
| `GET` | `/stats/services` | Yes | Detected service stats |
| `GET` | `/stats/services/debug` | Yes | Service detection diagnostics |
| `GET` | `/stats/stream` | Yes | SSE stream of live stats |
| `POST` | `/containers/{id}/start` | Yes | Start a Docker container |
| `POST` | `/containers/{id}/stop` | Yes | Stop a Docker container |
| `POST` | `/containers/{id}/restart` | Yes | Restart a Docker container |
| `POST` | `/processes/{pid}/kill` | Yes | Kill a process by PID |
| `POST` | `/services/{pluginId}/configure` | Yes | Set service credentials (e.g. Pi-hole password) |
| `POST` | `/agent/restart` | Yes | Restart agent via systemd |
| `POST` | `/agent/stop` | Yes | Stop agent via systemd |
| `GET` | `/agent/status` | Yes | Agent version and service state |

---

## Auth Handshake Flow

The macOS app uses a two-step verification when connecting to a server:

```
Step 1: GET /health  (no auth)
        ├─ No response / timeout → server.status = .offline
        └─ 200 OK → server is reachable, proceed to step 2

Step 2: GET /stats   (Bearer token)
        ├─ 401 Unauthorized → server.status = .unauthorized
        └─ 200 OK → server.status = .healthy (connection verified)
```

This handshake runs:
- **AddServerSheet** — "Connect" button runs handshake before saving. Token is required.
- **EditServerSheet** — Re-verifies only if host/port/token changed. Name-only edits skip.
- **DashboardView inline edit** — Same pattern via `testAndSaveEdit()`.
- **SSE stream** — On connect/reconnect, the app does a full `GET /stats` fetch, then opens `GET /stats/stream` for live updates. 401 sets `.unauthorized`, unreachable sets `.offline`.

### Agent Requirements

- `/health` must **never** require auth — the app uses it to distinguish "offline" from "unauthorized"
- `/stats` must return exactly `401` (not 403, not 200 with error body) when the token is wrong
- 401 response should have an empty body — the app only checks the HTTP status code

---

## GET /health

Lightweight check to determine if the agent is reachable. No auth required.

**Response** `200 OK`

```json
{
  "status": "ok"
}
```

---

## GET /stats

Full system stats and Docker container stats in a single response.

**Response** `200 OK`

```json
{
  "system": {
    "cpu": {
      "usagePercent": 42.5,
      "coreCount": 8,
      "temperature": 64.7
    },
    "memory": {
      "usedBytes": 22548578304,
      "totalBytes": 34359738368
    },
    "disk": {
      "usedBytes": 279172874240,
      "totalBytes": 536870912000
    },
    "network": {
      "downloadBytesPerSec": 13631488.0,
      "uploadBytesPerSec": 3145728.0
    },
    "uptimeSeconds": 1048962
  },
  "containers": [
    {
      "id": "a1b2c3d4e5f6",
      "name": "pihole",
      "image": "pihole/pihole:latest",
      "status": "running",
      "cpuPercent": 2.4,
      "memoryUsageMB": 142.5,
      "memoryLimitMB": 512.0,
      "networkRxBytes": 1048576000,
      "networkTxBytes": 524288000,
      "blockReadBytes": 2147483648,
      "blockWriteBytes": 1073741824,
      "pids": 12,
      "startedAt": "2025-01-15T08:30:00Z"
    }
  ]
}
```

**Error responses:**

| Status | Meaning |
|--------|---------|
| `401 Unauthorized` | Missing or invalid Bearer token (empty body) |
| `429 Too Many Requests` | Rate limit exceeded (60/min per IP) |

If Docker is not installed or the socket is unavailable, `containers` is an empty array `[]`.

---

## GET /stats/system

System stats only, without Docker overhead.

**Response** `200 OK` — same as `stats.system` above.

---

## GET /stats/docker

Docker container stats only.

**Response** `200 OK` — same as `stats.containers` above (array).

---

## POST /agent/restart

Restart the agent via systemd. The agent responds before restarting.

**Response** `200 OK`

```json
{
  "message": "restarting"
}
```

The agent process dies and systemd brings it back (~5 seconds). The macOS app will see a brief offline status, then `/health` returns and the green dot comes back.

---

## POST /agent/stop

Stop the agent via systemd. The agent responds before stopping.

**Response** `200 OK`

```json
{
  "message": "stopping"
}
```

After stopping, the agent is unreachable. The macOS app will show `.offline`.

---

## GET /agent/status

**Response** `200 OK`

```json
{
  "version": "0.1.0",
  "status": "active"
}
```

---

## Field Reference

### System Stats

| Field | Type | Unit | Description |
|-------|------|------|-------------|
| `cpu.usagePercent` | `float64` | `%` (0-100) | Overall CPU usage across all cores |
| `cpu.coreCount` | `int` | count | Number of logical CPU cores |
| `cpu.temperature` | `float64` | `°C` | CPU package temperature. `0` if unavailable |
| `memory.usedBytes` | `int64` | bytes | Used RAM (excluding buffers/cache) |
| `memory.totalBytes` | `int64` | bytes | Total physical RAM |
| `disk.usedBytes` | `int64` | bytes | Used space on root mount (`/`) |
| `disk.totalBytes` | `int64` | bytes | Total space on root mount (`/`) |
| `network.downloadBytesPerSec` | `float64` | bytes/sec | Current download rate across all interfaces |
| `network.uploadBytesPerSec` | `float64` | bytes/sec | Current upload rate across all interfaces |
| `uptimeSeconds` | `int` | seconds | System uptime since last boot |

### CPU Usage Calculation

The agent computes `usagePercent` by sampling `/proc/stat` at 1-second intervals and calculating the delta:

```
usage = (delta_total - delta_idle) / delta_total * 100
```

This runs in a background goroutine. The API always returns the latest computed value.

### Network Speed Calculation

The agent computes bytes-per-second by sampling `/proc/net/dev` at 1-second intervals and dividing the byte delta by the time delta. The app expects instantaneous speed, not cumulative totals.

### Temperature

Read from `/sys/class/thermal/thermal_zone*/temp`. Returns the highest value across all zones. Divided by 1000 (kernel reports millidegrees). Returns `0` if not available.

---

### Container Stats

| Field | Type | Unit | Description |
|-------|------|------|-------------|
| `id` | `string` | — | Short container ID (first 12 chars) |
| `name` | `string` | — | Container name (without leading `/`) |
| `image` | `string` | — | Full image name with tag |
| `status` | `string` | enum | `"running"`, `"stopped"`, or `"restarting"` |
| `cpuPercent` | `float64` | `%` (0-100+) | Container CPU usage. Can exceed 100% on multi-core |
| `memoryUsageMB` | `float64` | MB | Current memory usage |
| `memoryLimitMB` | `float64` | MB | Container memory limit. `0` if unlimited |
| `networkRxBytes` | `int64` | bytes | Total bytes received since container start |
| `networkTxBytes` | `int64` | bytes | Total bytes sent since container start |
| `blockReadBytes` | `int64` | bytes | Total bytes read from disk since container start |
| `blockWriteBytes` | `int64` | bytes | Total bytes written to disk since container start |
| `pids` | `int` | count | Current number of processes in the container |
| `startedAt` | `string` | ISO 8601 | Container start time. `null` if stopped |

### Container CPU Calculation

Docker provides `PreCPUStats` (previous sample) in each stats response. The agent calculates:

```
cpuPercent = (delta_container_cpu / delta_system_cpu) * numCores * 100
```

### Container Status Mapping

| Docker State | Agent Value |
|-------------|-------------|
| `running` | `"running"` |
| `exited`, `dead`, `created` | `"stopped"` |
| `restarting` | `"restarting"` |
| `paused` | `"stopped"` |

### startedAt Format

ISO 8601 with timezone: `"2025-01-15T08:30:00Z"`

The macOS app decodes this with `ISO8601DateFormatter` and computes uptime client-side.

---

## Server Status Derivation

The macOS app determines server status from the connection state and stats response:

| Condition | Status | Icon |
|-----------|--------|------|
| No response / timeout | `.offline` | — |
| `/health` OK but `/stats` returns 401 | `.unauthorized` | Lock, orange |
| CPU > 90% OR Memory > 95% | `.critical` | Red |
| CPU > 75% OR Memory > 85% | `.warning` | Yellow |
| Otherwise | `.healthy` | Green |

The agent does not send a status field. The app computes it.

---

## Error Handling

| Scenario | Agent Response | App Behavior |
|----------|---------------|-------------|
| Agent unreachable | No response | `.offline` |
| Wrong/missing Bearer token | `401` (empty body) | `.unauthorized` |
| Rate limit exceeded | `429` | Retries after backoff |
| Docker not installed | `containers: []` | Shows empty container list |
| Docker socket denied | `containers: []` | Shows empty container list |
| Temperature unavailable | `temperature: 0` | Shows 0 or hides |
| Container memory unlimited | `memoryLimitMB: 0` | Shows as unlimited |

---

## GET /stats/stream (SSE)

Server-Sent Events endpoint for live stats. The macOS app opens a persistent connection and receives events as they're produced by the agent's background collectors.

**Headers:** `Content-Type: text/event-stream`

**Auth:** Same `Authorization: Bearer <token>` header as other endpoints.

### Event Types

**`system`** — Fires every **1 second**. Contains system stats + top processes.

```
event: system
data: {"system":{"cpu":{"usagePercent":42.5,...},"memory":{...},...},"processes":[...]}
```

**`docker`** — Fires every **5 seconds**. Contains all container stats.

```
event: docker
data: [{"id":"a1b2c3","name":"pihole","status":"running",...},...]
```

**`services`** — Fires every **10 seconds**. Contains detected service stats.

```
event: services
data: [{"pluginId":"pihole","name":"Pi-hole","status":"running",...},...]
```

**Keepalive** — Comment line every **30 seconds** to prevent proxy timeouts.

```
: keepalive
```

### Connection Lifecycle

1. App opens `GET /stats/stream` with Bearer auth
2. Agent validates token (401 if invalid)
3. Agent holds connection open, streams events
4. On disconnect (network, proxy timeout, agent restart), app reconnects with exponential backoff (2s → 4s → 8s → max 30s)
5. On reconnect, app does a full `GET /stats` fetch first to fill UI, then reopens the stream

### Proxy Compatibility

- `X-Accel-Buffering: no` header disables nginx/reverse proxy buffering
- `Cache-Control: no-cache` prevents intermediate caching
- 30s keepalive pings prevent Cloudflare tunnel idle timeouts (~100s)
- Server's WriteTimeout is disabled for this endpoint

---

## Container & Process Actions

### POST /containers/{id}/start|stop|restart

Control Docker containers. Returns a message on success.

**Response** `200 OK`

```json
{
  "message": "started"
}
```

### POST /processes/{pid}/kill

Kill a process by PID (sends SIGTERM).

**Response** `200 OK`

```json
{
  "message": "killed"
}
```

---

## POST /services/{pluginId}/configure

Configure credentials for a detected service. Currently used for Pi-hole v6 password authentication.

**Request** `POST /services/pihole/configure`

```json
{
  "password": "your-pihole-password"
}
```

**Response** `200 OK`

```json
{
  "message": "configured"
}
```

The agent stores the password in its config file (`/etc/deskmon/config.yaml`) and uses it to authenticate with the Pi-hole v6 API on the next collection cycle. The password persists across agent restarts.

### Pi-hole v6 Authentication Flow

Pi-hole v6 requires session-based authentication for detailed stats:

1. Agent sends `POST /api/auth` to Pi-hole with the password
2. Pi-hole returns a session ID (SID) valid for ~5 minutes
3. Agent uses `X-FTL-SID` header on subsequent API requests
4. Sessions auto-renew on each successful request
5. On session expiry (401), agent re-authenticates automatically

If no password is configured, the agent returns `authRequired: true` in the Pi-hole service stats, and the macOS app shows a password prompt.

### Agent Config

Pi-hole password can also be set directly in `/etc/deskmon/config.yaml`:

```yaml
port: 7654
auth_token: "your-agent-token"
services:
  pihole:
    password: "your-pihole-password"
```

---

## Implemented Container Fields

- `containers[].ports` — Array of port mappings `[{"hostPort": 8080, "containerPort": 80, "protocol": "tcp"}]`
- `containers[].restartCount` — Number of container restarts
- `containers[].healthStatus` — `"healthy"`, `"unhealthy"`, `"starting"`, `"none"`
