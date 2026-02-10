# Deskmon Agent — Implementation Design

**Date:** 2026-02-10
**Status:** Approved
**Scope:** System stats + Docker stats + Agent control via systemd

---

## Overview

Deskmon Agent is a lightweight Go binary that runs on Linux servers, collects system and Docker metrics, and exposes them over HTTP. The macOS Deskmon app polls the agent at a configurable interval (default 3s) to display live server stats. Users can also control the agent (restart, stop) directly from the macOS app.

The agent is designed as a deploy-and-forget service: one-command install, auto-start on boot, auto-recover from crashes.

---

## Architecture

```
macOS App (SwiftUI)                    Linux Server
┌─────────────────────┐                ┌──────────────────────────────────┐
│                     │  GET /health   │                                  │
│  Polling ON (3s)  ──┼───────────────►│  deskmon-agent                   │
│                     │  GET /stats    │  ├─ HTTP Server (:7654)          │
│  Server Dashboard ◄─┼───────────────►│  ├─ Auth Middleware (Bearer)     │
│                     │                │  ├─ Background Sampler (1s tick) │
│  Restart Agent    ──┼──POST─────────►│  │  ├─ CPU from /proc/stat      │
│                     │ /agent/restart │  │  ├─ Network from /proc/net/dev│
│  Settings           │                │  │  └─ Stores deltas in memory   │
│  ├─ Polling toggle  │                │  ├─ On-request collectors        │
│  ├─ Refresh interval│                │  │  ├─ Memory from /proc/meminfo │
│  └─ Server list     │                │  │  ├─ Disk via syscall.Statfs   │
└─────────────────────┘                │  │  ├─ Temp from /sys/class/     │
                                       │  │  ├─ Uptime from /proc/uptime  │
                                       │  │  └─ Docker via socket         │
                                       │  └─ Systemctl wrapper            │
                                       │     ├─ restart (hardcoded)       │
                                       │     └─ stop (hardcoded)          │
                                       │                                  │
                                       │  systemd: deskmon-agent.service  │
                                       │  ├─ Restart=always               │
                                       │  ├─ After=network,docker         │
                                       │  └─ ProtectSystem=strict         │
                                       └──────────────────────────────────┘
```

---

## Project Structure

```
deskmon-agent/
├── cmd/deskmon-agent/
│   └── main.go              # Entry point, config loading, server startup
├── internal/
│   ├── api/
│   │   ├── server.go        # HTTP server, router, middleware (auth, rate limit)
│   │   ├── handlers.go      # /health, /stats endpoint handlers
│   │   └── control.go       # /agent/stop, /agent/restart, /agent/status handlers
│   ├── collector/
│   │   ├── system.go        # CPU, memory, disk, network, temp, uptime
│   │   └── docker.go        # Container stats via Docker socket
│   ├── config/
│   │   └── config.go        # YAML config loading + defaults
│   └── systemctl/
│       └── systemctl.go     # Wrapper to exec hardcoded systemctl commands
├── scripts/
│   └── install.sh           # Server-side install + systemd setup
├── Makefile
├── go.mod
├── README.md
├── agent-api-contract.md
└── LICENSE
```

### Key Decisions

- `internal/` keeps all packages private — this is a binary, not a library
- `collector` package handles all metrics, separated into system vs Docker
- `systemctl` package is a thin wrapper around `os/exec` with hardcoded commands only
- `api` package owns HTTP routing, auth middleware, rate limiting, and handlers
- `config` package loads `/etc/deskmon/config.yaml` with baked-in defaults
- No external web framework — Go `net/http` stdlib is sufficient
- Single external dependency: `github.com/docker/docker/client` for container stats

---

## System Metrics Collection

All system metrics are read directly from Linux virtual filesystems. No shelling out to external commands.

### CPU Usage — Delta sampling from `/proc/stat`

- Background goroutine reads the `cpu` line every 1 second for total jiffies
- Stores previous sample; calculates: `usage% = (totalDelta - idleDelta) / totalDelta * 100`
- API always returns the latest computed value — no request-time blocking

### Memory — Single read from `/proc/meminfo`

- `totalBytes` = MemTotal
- `usedBytes` = MemTotal - MemAvailable (accounts for buffers/cache correctly)

### Disk — `syscall.Statfs()` on `/`

- `totalBytes` and `usedBytes` from the root mount
- No file parsing needed

### Network — Delta sampling from `/proc/net/dev`

- Sum RX/TX bytes across all non-loopback interfaces
- Store previous sample + timestamp; compute `bytesPerSec = bytesDelta / timeDelta`
- Same background goroutine as CPU (1s tick)

### Temperature — Read from `/sys/class/thermal/thermal_zone*/temp`

- Scan all thermal zones, pick the highest value
- Divide by 1000 (kernel reports millidegrees)
- Return 0 if no zones found (per API contract)

### Uptime — Single read from `/proc/uptime`

- First field is seconds since boot as a float, truncated to integer

---

## Docker Stats Collection

Uses the official Docker Go SDK (`github.com/docker/docker/client`) via `/var/run/docker.sock`.

### Container Discovery

- `client.ContainerList()` with `all: true` — returns running and stopped containers
- Maps to API contract fields: `id`, `name`, `image`, `status` (normalized to "running"/"stopped"/"restarting")

### Container Resource Stats

- For running containers only: `client.ContainerStats()` with `stream: false` (one-shot)
- Extracted metrics:
  - `memoryUsageMB` = usage / 1024 / 1024
  - `memoryLimitMB` = limit / 1024 / 1024 (0 if unlimited)
  - `networkRxBytes`, `networkTxBytes` = sum across all networks
  - `blockReadBytes`, `blockWriteBytes` = sum from BlkioStats
  - `pids` = current PID count

### CPU Percentage — Per-container delta calculation

- Store previous `cpu_usage.total_usage` and `system_cpu_usage` per container ID
- `cpuPercent = (containerDelta / systemDelta) * numCores * 100`
- First read for a new container returns 0 (no previous sample)

### Graceful Degradation

- Docker not installed (socket missing) → return empty `containers: []`
- Permission denied on socket → return empty `containers: []`
- Individual container stats fail → skip that container, return the rest
- No errors bubble up to the API response

### Collection Strategy

- Docker stats are collected **on-request** (not in the background sampler)
- The Docker API handles its own caching; calling it every 3s (macOS app polling interval) is lightweight
- Stopped containers return zeroes for all resource fields

---

## HTTP API

### Endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| `GET` | `/health` | No | `{"status": "ok"}` — online detection |
| `GET` | `/stats` | Yes | Full system + Docker stats |
| `GET` | `/stats/system` | Yes | System stats only |
| `GET` | `/stats/docker` | Yes | Docker container stats only |
| `POST` | `/agent/restart` | Yes | Restart agent via systemd |
| `POST` | `/agent/stop` | Yes | Stop agent via systemd |
| `GET` | `/agent/status` | Yes | Agent version + service state |

### Auth Middleware

- All endpoints except `/health` require `Authorization: Bearer <token>` header
- Token validated with `crypto/subtle.ConstantTimeCompare()` (timing-attack safe)
- Missing or invalid token → `401 Unauthorized` with no body detail

### Polling Model

The agent is stateless with respect to polling. The macOS app controls when and how often to call.

- **Polling ON (3s):** App hits `GET /stats` every 3s, gets latest sampled data
- **Polling OFF:** App stops calling; agent keeps sampling silently in background
- **Restart Agent:** `POST /agent/restart` → agent dies, systemd restarts it → brief offline, then `/health` returns → green dot

### Background Sampler

- Goroutine ticks every 1 second, updating CPU and network delta calculations
- Runs regardless of polling state — first request after a pause gets accurate data immediately
- System metrics that don't need deltas (memory, disk, temp, uptime) are read on-request

---

## Agent Control via Systemd

The `systemctl` package wraps exactly two commands:

```go
// These are the ONLY commands the agent can execute
exec.Command("systemctl", "restart", "deskmon-agent")
exec.Command("systemctl", "stop", "deskmon-agent")
```

- No string interpolation, no user-supplied arguments
- `/agent/restart` → agent process stops, systemd brings it back within 5 seconds
- `/agent/stop` → agent process stops, stays stopped. macOS app sees "offline"
- `/agent/status` → reads service state without exec (checks own PID + version)

---

## Security Hardening

**Principle: The agent is a read-only stats reporter with exactly 2 hardcoded control actions. Nothing else.**

### Authentication & Access

- Bearer token required on all endpoints except `/health`
- Token comparison uses `crypto/subtle.ConstantTimeCompare()` — prevents timing attacks
- `/health` returns only `{"status": "ok"}` — leaks zero information without auth
- Rate limiting: 60 requests/minute per IP; exceeding returns `429 Too Many Requests`; prevents brute-force token guessing

### Zero Injection Surface

- No user input is ever passed to any shell command, file path, or system call
- Control endpoints execute **hardcoded strings only** — no interpolation, no parameters
- No query parameters, no request body parsing, no URL path parameters that reach the OS
- All file reads target hardcoded paths: `/proc/stat`, `/proc/meminfo`, `/proc/net/dev`, `/sys/class/thermal/`, `/proc/uptime`

### Network Hardening

- Configurable bind address — default `0.0.0.0`, can restrict to specific interface
- Request body size capped at 1KB (control endpoints need no body, stats endpoints are GET-only)
- Read timeout: 10s, Write timeout: 10s — prevents slowloris attacks
- POST only accepted on `/agent/*` routes; all other POST/PUT/DELETE/PATCH return `405 Method Not Allowed`
- Only `GET` and `POST` methods are routed; everything else is rejected

### Process Isolation

- Agent only reads from `/proc`, `/sys`, and Docker socket — no filesystem writes
- No outbound network connections — no phoning home, no telemetry, no update checks
- Docker client is read-only: `ContainerList` and `ContainerStats` only. No exec, no create, no delete, no image pull
- Systemd unit enforces kernel-level protection:
  - `ProtectSystem=strict` — mounts filesystem read-only
  - `ReadOnlyPaths=/` — reinforces read-only access
  - `ProtectHome=yes` — blocks access to /home
  - `NoNewPrivileges=yes` — prevents privilege escalation
  - `PrivateTmp=yes` — isolated temp directory

### Error Response Sanitization

- Generic error messages only — never exposes internal paths, stack traces, or system details
- Failed auth: `401 Unauthorized` with empty body
- Unknown routes: `404 Not Found` with no server fingerprint
- Internal errors: `500 Internal Server Error` with no detail (logged internally only)
- Server header stripped — does not advertise Go version or framework

### Secrets Management

- Auth token stored in `/etc/deskmon/config.yaml` with `0600` permissions (root-only read)
- Token never logged, never included in error responses, never exposed via any endpoint
- Config directory `/etc/deskmon/` owned by root with `0700` permissions

---

## Build & Deployment

### Makefile Targets

| Target | Purpose |
|--------|---------|
| `make build` | Build for current OS (development) |
| `make build-linux-amd64` | Cross-compile for Linux x86_64 |
| `make build-linux-arm64` | Cross-compile for Linux ARM64 |
| `make build-all` | Both Linux architectures |
| `make package-amd64` | Bundle binary + install.sh → `dist/deskmon-agent-linux-amd64.tar.gz` |
| `make package-arm64` | Bundle binary + install.sh → `dist/deskmon-agent-linux-arm64.tar.gz` |
| `make package-all` | Both packages |
| `make test` | Run Go tests |
| `make clean` | Remove `bin/` and `dist/` |

### install.sh

```
sudo ./install.sh                    # defaults: port 7654
sudo ./install.sh --port 9090        # custom port
sudo ./install.sh --uninstall        # remove everything
```

**What it does on install:**

1. Copies `deskmon-agent` binary to `/usr/local/bin/`
2. Creates `/etc/deskmon/` directory (0700, root-owned)
3. Generates random 32-character auth token
4. Writes `/etc/deskmon/config.yaml` (0600) with token + port
5. Creates `/etc/systemd/system/deskmon-agent.service`
6. Runs `systemctl daemon-reload`
7. Runs `systemctl enable deskmon-agent` (auto-start on boot)
8. Runs `systemctl start deskmon-agent`
9. Prints summary: port, auth token, firewall reminder

**What it does on uninstall:**

1. Stops the service
2. Disables the service
3. Removes the unit file
4. Removes `/usr/local/bin/deskmon-agent`
5. Removes `/etc/deskmon/` directory
6. Runs `systemctl daemon-reload`

### Systemd Unit File

```ini
[Unit]
Description=Deskmon Agent - System Monitoring
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/deskmon-agent
Restart=always
RestartSec=5
User=root

# Security hardening
ProtectSystem=strict
ReadOnlyPaths=/
ProtectHome=yes
NoNewPrivileges=yes
PrivateTmp=yes

# Allow reading system stats and Docker socket
ReadWritePaths=/var/run/docker.sock

[Install]
WantedBy=multi-user.target
```

---

## Configuration

### `/etc/deskmon/config.yaml`

```yaml
port: 7654
auth_token: "a1b2c3d4e5f6..."
```

Two fields only. Everything else uses internal defaults:

| Setting | Default | Notes |
|---------|---------|-------|
| `port` | `7654` | Configurable via install.sh or config edit |
| `auth_token` | (generated) | 32-char random string |
| Bind address | `0.0.0.0` | Internal default |
| Sample interval | `1s` | Internal default, not configurable |
| Docker socket | `/var/run/docker.sock` | Internal default |

---

## User Workflow

### Initial Setup

```
# On your Mac (or any build machine with Go):
make package-amd64

# Copy to server:
scp dist/deskmon-agent-linux-amd64.tar.gz user@server:~/

# On the server:
tar xzf deskmon-agent-linux-amd64.tar.gz
cd deskmon-agent
sudo ./install.sh --port 7654

# Output:
# ✓ deskmon-agent installed to /usr/local/bin/
# ✓ Config written to /etc/deskmon/config.yaml
# ✓ Service enabled and started
#
# ─────────────────────────────────────
# Port:       7654
# Auth Token: a1b2c3d4e5f6g7h8i9j0...
# ─────────────────────────────────────
#
# Add this server to your Deskmon macOS app:
#   Address: <server-ip>:7654
#   Token:   a1b2c3d4e5f6g7h8i9j0...
```

### Day-to-Day

- Stats flow automatically — macOS app polls, agent responds
- Agent survives reboots (systemd enabled)
- Agent recovers from crashes (systemd Restart=always)
- User can restart/stop agent from macOS app settings

---

## Out of Scope (Intentional)

- **Integrations** (Pihole, Plex, Jellyfin, Home Assistant) — layer in later
- **WebSocket log streaming** — future feature per API contract
- **Config changes from macOS app** — edit YAML on server for now
- **TLS termination** — use a reverse proxy or VPN
- **Multi-user auth** — single shared token per server
- **Automatic updates** — user manually deploys new versions
