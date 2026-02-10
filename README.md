# deskmon-agent

Lightweight system monitoring agent for Linux servers. Collects system and Docker stats, exposes them via HTTP API, and is controllable from the Deskmon macOS app.

Single binary. One-command install. Set it and forget it.

![License](https://img.shields.io/badge/license-MIT-green)
![Platform](https://img.shields.io/badge/platform-Linux-blue)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8)

---

## How It Works

```
┌─────────────────────────────────────────────────────────┐
│                     Your Mac                            │
│  ┌───────────────────────────────────────────────────┐  │
│  │         Deskmon macOS App (SwiftUI)               │  │
│  │                                                   │  │
│  │  Polls agents at your configured interval (3s).   │  │
│  │  Renders CPU, RAM, disk, network, containers.     │  │
│  │  Controls agent: restart, stop.                   │  │
│  └───────────────────────────────────────────────────┘  │
└───────────────────────┬─────────────────────────────────┘
                        │ HTTP (Bearer token auth)
                        ▼
┌─────────────────────────────────────────────────────────┐
│                   Your Linux Server                     │
│  ┌───────────────────────────────────────────────────┐  │
│  │         deskmon-agent (this project)              │  │
│  │                                                   │  │
│  │  Reads /proc and /sys for system metrics          │  │
│  │  Queries Docker socket for container stats        │  │
│  │  Serves JSON on a single port (default 7654)      │  │
│  │  Managed by systemd — auto-start, auto-recover    │  │
│  │                                                   │  │
│  │  No cloud. No relay. Direct connection.           │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

---

## Quick Start

### Prerequisites

- Linux server (Ubuntu, Debian, Fedora, etc.)
- Go 1.21+ installed (`sudo apt install golang-go`)
- Git installed

### Install

```bash
git clone https://github.com/neur0map/deskmon-agent.git
cd deskmon-agent
sudo make setup
```

That's it. One command builds the binary, installs it, generates an auth token, creates a systemd service, and starts the agent.

**Custom port:**

```bash
sudo make setup PORT=9090
```

**Expected output:**

```
Detected: Linux x86_64 (amd64)
Building deskmon-agent v0.1.0...
Build complete: bin/deskmon-agent

Installing deskmon-agent...
  Binary: bin/deskmon-agent
  Installed binary to /usr/local/bin/deskmon-agent
  Config written to /etc/deskmon/config.yaml
  Service file created
  Service enabled and started

===========================================
  deskmon-agent installed successfully
===========================================

  Port:       7654
  Auth Token: a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
  Config:     /etc/deskmon/config.yaml
  Service:    deskmon-agent

  Add this server to your Deskmon macOS app:
    Address: 192.168.1.100:7654
    Token:   a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6

  Useful commands:
    systemctl status deskmon-agent
    journalctl -u deskmon-agent -f

  Firewall reminder:
    sudo ufw allow 7654/tcp
===========================================
```

### Connect from macOS App

1. Open Deskmon on your Mac
2. Go to **Settings** > **+ Add Server**
3. Enter the server address and port (e.g. `192.168.1.100:7654`)
4. Enter the auth token printed during install
5. Green dot = connected

### Verify Manually

```bash
# Health check (no auth required)
curl http://localhost:7654/health

# Full stats (auth required)
curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:7654/stats
```

---

## What You Get

Once running, the macOS app shows live stats for each server:

- **CPU** — Usage %, core count, temperature
- **Memory** — Used / total RAM
- **Disk** — Used / total on root mount
- **Network** — Download/upload speed (bytes/sec)
- **Uptime** — Time since last boot
- **Docker containers** — Per-container CPU, memory, network, block I/O, PIDs, status

The agent's background sampler ticks every 1 second, so data is always fresh regardless of your polling interval.

---

## API Endpoints

All endpoints except `/health` require `Authorization: Bearer <token>`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | `{"status": "ok"}` — online detection |
| `GET` | `/stats` | Full system + Docker container stats |
| `GET` | `/stats/system` | System stats only (no Docker overhead) |
| `GET` | `/stats/docker` | Docker container stats only |
| `POST` | `/agent/restart` | Restart agent via systemd |
| `POST` | `/agent/stop` | Stop agent via systemd |
| `GET` | `/agent/status` | Agent version and service state |

See [agent-api-contract.md](agent-api-contract.md) for the full JSON schema and field reference.

---

## Configuration

Config is auto-generated during install at `/etc/deskmon/config.yaml`:

```yaml
port: 7654
auth_token: "your-generated-token"
```

To change settings, edit the file and restart:

```bash
sudo nano /etc/deskmon/config.yaml
sudo systemctl restart deskmon-agent
```

Or restart from the macOS app's Settings panel.

---

## Day-to-Day Operations

### Upgrades

```bash
cd deskmon-agent
git pull
sudo make setup
```

Rebuilds the binary, replaces it, restarts the service. Your existing config and auth token are preserved.

### Uninstall

```bash
sudo make uninstall
```

Stops the service, removes the binary, config, and systemd unit file.

### Useful Commands

```bash
# Check agent status
systemctl status deskmon-agent

# View logs
journalctl -u deskmon-agent -f

# Restart manually
sudo systemctl restart deskmon-agent
```

---

## Agent Control from macOS

The macOS app can control the agent remotely:

- **Restart Agent** — Sends `POST /agent/restart`. Agent restarts via systemd (~5 seconds). You'll see a brief offline status, then it comes back.
- **Polling toggle** — Turns HTTP polling on/off from the app side. Agent keeps running either way.
- **Refresh interval** — Controls how often the app hits `/stats` (e.g. 3s, 5s, 10s).

The agent auto-recovers from crashes (systemd `Restart=always`) and starts automatically on server reboot.

---

## Security

The agent is hardened as a read-only stats reporter:

- **Auth token** — Auto-generated 32-char token, constant-time comparison to prevent timing attacks
- **Rate limiting** — 60 requests/minute per IP to prevent brute-force
- **No injection surface** — Zero user input reaches shell commands, file paths, or system calls. Control endpoints execute hardcoded `systemctl` commands only.
- **Read-only** — Only reads from `/proc`, `/sys`, and Docker socket. No filesystem writes. Docker client is read-only (list and stats only).
- **No outbound connections** — No phoning home, no telemetry, no update checks
- **Systemd sandboxing** — `ProtectSystem=strict`, `ReadOnlyPaths=/`, `ProtectHome=yes`, `NoNewPrivileges=yes`
- **Config permissions** — `/etc/deskmon/` is root-only (0700), config file is 0600

### Firewall

Open only the agent port:

```bash
sudo ufw allow 7654/tcp
```

For extra security, restrict to your Mac's IP:

```bash
sudo ufw allow from 192.168.1.50 to any port 7654
```

---

## Cross-Compile (Alternative Deployment)

If you prefer not to install Go on the server, build on your Mac and copy the package:

```bash
# On your Mac
make package-amd64    # x86_64 servers
make package-arm64    # ARM servers

# Copy to server
scp dist/deskmon-agent-0.1.0-linux-amd64.tar.gz user@server:~/

# On the server
tar xzf deskmon-agent-0.1.0-linux-amd64.tar.gz
cd deskmon-agent
sudo ./install.sh
sudo ./install.sh --port 9090    # custom port
```

---

## Project Structure

```
deskmon-agent/
├── cmd/deskmon-agent/
│   └── main.go              # Entry point, graceful shutdown
├── internal/
│   ├── api/
│   │   ├── server.go        # HTTP server, auth, rate limiting
│   │   ├── handlers.go      # /health, /stats handlers
│   │   ├── control.go       # /agent/* control handlers
│   │   └── server_test.go   # API tests
│   ├── collector/
│   │   ├── system.go        # CPU, memory, disk, network, temp, uptime
│   │   └── docker.go        # Container stats via Docker SDK
│   ├── config/
│   │   ├── config.go        # YAML config loader
│   │   └── config_test.go   # Config tests
│   └── systemctl/
│       └── systemctl.go     # Hardcoded systemctl commands
├── scripts/
│   └── install.sh           # Server install/uninstall script
├── docs/plans/              # Design documents
├── Makefile                 # Build, setup, package, test
├── agent-api-contract.md    # API contract (source of truth)
└── README.md
```

---

## Troubleshooting

**Agent not starting**
```bash
journalctl -u deskmon-agent -f
```

**Port already in use**
```bash
ss -tlnp | grep 7654
```

**Docker containers not showing**
- Agent runs as root, so Docker socket access should work
- Check Docker is running: `systemctl status docker`
- If no Docker installed, `containers` will be an empty array (not an error)

**Temperature showing 0**
- Some VMs and cloud servers don't expose thermal zones
- Agent returns `0` gracefully per the API contract

**Forgot auth token**
```bash
sudo cat /etc/deskmon/config.yaml
```

---

## Makefile Reference

| Target | Description |
|--------|-------------|
| `sudo make setup` | Build + install + start (on Linux server) |
| `sudo make setup PORT=9090` | Same, with custom port |
| `sudo make uninstall` | Remove everything |
| `make build` | Build for current OS |
| `make build-all` | Cross-compile both Linux architectures |
| `make package-amd64` | Package for Linux x86_64 |
| `make package-arm64` | Package for Linux ARM64 |
| `make test` | Run tests |
| `make clean` | Remove build artifacts |

---

## License

MIT License — see [LICENSE](LICENSE) for details.
