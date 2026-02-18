# Deskmon Agent API Contract

What the macOS app expects from `deskmon-agent`. This is the source of truth for the JSON shapes the Swift client will decode.

**Default port:** `7654`
**Bind:** `127.0.0.1` (localhost only)
**Auth:** None — the agent is only reachable via SSH tunnel. The macOS app handles SSH authentication.

---

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Reachability check |
| `GET` | `/stats` | Full system + container stats |
| `GET` | `/stats/system` | System stats only |
| `GET` | `/stats/docker` | Docker container stats only |
| `GET` | `/stats/processes` | Top processes by CPU |
| `GET` | `/stats/stream` | SSE stream of live stats |
| `POST` | `/containers/{id}/start` | Start a Docker container |
| `POST` | `/containers/{id}/stop` | Stop a Docker container |
| `POST` | `/containers/{id}/restart` | Restart a Docker container |
| `POST` | `/processes/{pid}/kill` | Kill a process by PID |
| `POST` | `/agent/restart` | Restart agent via systemd |
| `POST` | `/agent/stop` | Stop agent via systemd |
| `GET` | `/agent/status` | Agent version and service state |

---

## Connection Flow

The macOS app connects to the agent via SSH tunnel:

```
Step 1: SSH connect to server (password or ed25519 key)
Step 2: Open SSH tunnel → localhost:7654 on the server
Step 3: GET /stats via tunnel to verify agent is running
Step 4: Open GET /stats/stream for live updates
```

The agent binds to `127.0.0.1` only and requires no API authentication. SSH handles all security. On first connect, the app authenticates with SSH password, then auto-generates an ed25519 key and installs it on the server for subsequent connections.

On disconnect, the app reconnects with exponential backoff (2s → 4s → 8s → max 30s).

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
      "startedAt": "2025-01-15T08:30:00Z",
      "ports": [
        { "hostPort": 8080, "containerPort": 80, "protocol": "tcp" }
      ],
      "restartCount": 0,
      "healthStatus": "healthy"
    }
  ],
  "processes": [
    {
      "pid": 1234,
      "name": "node",
      "cpuPercent": 15.2,
      "memoryMB": 256.5,
      "memoryPercent": 1.5,
      "command": "/usr/bin/node server.js",
      "user": "www-data"
    }
  ],
}
```

**Error responses:**

| Status | Meaning |
|--------|---------|
| `429 Too Many Requests` | Rate limit exceeded (60/min per IP) |

If Docker is not installed or the socket is unavailable, `containers` is an empty array `[]`.

---

## GET /stats/system

System stats only, without Docker overhead.

**Response** `200 OK` — same shape as `stats.system` above.

---

## GET /stats/docker

Docker container stats only.

**Response** `200 OK` — same shape as `stats.containers` above (array).

---

## GET /stats/processes

Top 10 processes sorted by CPU usage. CPU values are EMA-smoothed (alpha=0.3) for stability.

**Response** `200 OK`

```json
[
  {
    "pid": 1234,
    "name": "node",
    "cpuPercent": 15.2,
    "memoryMB": 256.5,
    "memoryPercent": 1.5,
    "command": "/usr/bin/node server.js",
    "user": "www-data"
  }
]
```

| Field | Type | Description |
|-------|------|-------------|
| `pid` | `int32` | Process ID |
| `name` | `string` | Process name from `/proc/<pid>/stat` |
| `cpuPercent` | `float64` | EMA-smoothed CPU usage percentage |
| `memoryMB` | `float64` | Resident memory in MB |
| `memoryPercent` | `float64` | Memory as percentage of total RAM |
| `command` | `string` | Full command line. Omitted if empty |
| `user` | `string` | Process owner username. Omitted if unresolvable |

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

**Docker mode:** Returns `400 Bad Request` with an error message. Systemctl is not available inside a container.

```json
{
  "error": "agent control not available in Docker mode — use docker restart instead"
}
```

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

**Docker mode:** Returns `400 Bad Request` with the same error as `/agent/restart`.

---

## GET /agent/status

**Response** `200 OK`

```json
{
  "version": "0.1.0",
  "status": "active"
}
```

**Docker mode:** Returns `"running (docker)"` as the status value.

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

In Docker mode, reads from `$DESKMON_HOST_SYS/class/thermal/thermal_zone*/temp` (typically `/host/sys/...`) to access host thermal zones instead of the container's isolated sysfs.

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
| SSH tunnel down / no response | `.offline` | — |
| CPU > 90% OR Memory > 95% | `.critical` | Red |
| CPU > 75% OR Memory > 85% | `.warning` | Yellow |
| Otherwise | `.healthy` | Green |

The agent does not send a status field. The app computes it.

---

## Error Handling

| Scenario | Agent Response | App Behavior |
|----------|---------------|-------------|
| SSH tunnel down | No response | `.offline` |
| Agent not running | No response on tunnel | `.offline` |
| Rate limit exceeded | `429` | Retries after backoff |
| Docker not installed | `containers: []` | Shows empty container list |
| Docker socket denied | `containers: []` | Shows empty container list |
| Temperature unavailable | `temperature: 0` | Shows 0 or hides |
| Container memory unlimited | `memoryLimitMB: 0` | Shows as unlimited |

---

## GET /stats/stream (SSE)

Server-Sent Events endpoint for live stats. The macOS app opens a persistent connection and receives events as they're produced by the agent's background collectors.

**Headers:** `Content-Type: text/event-stream`

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

**Keepalive** — Comment line every **15 seconds** to prevent proxy timeouts.

```
: keepalive
```

### Connection Lifecycle

1. App opens `GET /stats/stream` via SSH tunnel
2. Agent holds connection open, streams events
3. On disconnect (SSH drop, agent restart), app reconnects with exponential backoff (2s → 4s → 8s → max 30s)
4. On reconnect, app does a full `GET /stats` fetch first to fill UI, then reopens the stream

### Proxy Compatibility

- `X-Accel-Buffering: no` header disables nginx/reverse proxy buffering
- `Cache-Control: no-cache` prevents intermediate caching
- 15s keepalive pings prevent Cloudflare tunnel idle timeouts (~100s)
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


