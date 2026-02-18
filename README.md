# deskmon-agent

Lightweight system monitoring agent for Linux servers. Collects system and Docker stats, exposes them via HTTP API, and is controllable from the Deskmon macOS app.

Single binary. One-command install. Set it and forget it.

![License](https://img.shields.io/badge/license-MIT-green)
![Platform](https://img.shields.io/badge/platform-Linux-blue)
![Go](https://img.shields.io/badge/Go-1.22+-00ADD8)

---

## How It Works

```
┌─────────────────────────────────────────────────────────┐
│                     Your Mac                            │
│  ┌───────────────────────────────────────────────────┐  │
│  │         Deskmon macOS App (SwiftUI)               │  │
│  │                                                   │  │
│  │  Connects via SSH tunnel for live streaming stats.  │  │
│  │  Renders CPU, RAM, disk, network, containers.     │  │
│  │  Controls agent: restart, stop, container mgmt.   │  │
│  └───────────────────────────────────────────────────┘  │
└───────────────────────┬─────────────────────────────────┘
                        │ SSH tunnel → localhost:7654
                        ▼
┌─────────────────────────────────────────────────────────┐
│                   Your Linux Server                     │
│  ┌───────────────────────────────────────────────────┐  │
│  │         deskmon-agent (this project)              │  │
│  │                                                   │  │
│  │  Reads /proc and /sys for system metrics          │  │
│  │  Queries Docker socket for container stats        │  │
│  │  Serves JSON on localhost:7654 (not exposed)       │  │
│  │  Runs as systemd service or Docker container      │  │
│  │                                                   │  │
│  │  No cloud. No relay. No tokens. SSH handles auth. │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

---

## Quick Start

Three ways to install. Pick the one that matches your server.

| Method | Best for | Requirements |
|--------|----------|-------------|
| [Docker](#option-a-docker) | **unRAID**, TrueNAS, any server with Docker | Docker installed |
| [Prebuilt binary](#option-b-prebuilt-binary) | Ubuntu, Debian, Fedora, Arch | Terminal access + sudo |
| [Build from source](#option-c-build-from-source) | Developers, custom builds | Go 1.22+ and git |

---

### Option A: Docker

**This is the recommended install method for unRAID users.** No compiling, no Go, no systemd. Just Docker.

#### Step 1: Open a terminal on your server

- **unRAID:** Open your unRAID web UI (e.g. `http://tower.local`). Click the terminal icon (top-right `>_` button). A black terminal window opens in your browser. You type commands here.
- **Other Linux:** Open a terminal or SSH into your server.

#### Step 2: Copy and paste this entire command

Copy everything below (all 10 lines are one command), paste it into the terminal, and press Enter:

```bash
docker run -d \
  --name deskmon-agent \
  --pid=host \
  --network=host \
  -v /:/hostfs:ro,rslave \
  -v /sys:/host/sys:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /etc/deskmon:/etc/deskmon \
  -e DESKMON_HOST_ROOT=/hostfs \
  -e DESKMON_HOST_SYS=/host/sys \
  --restart unless-stopped \
  ghcr.io/neur0map/deskmon-agent:latest
```

You should see Docker pull the image and print a long container ID. That means it's running.

#### Step 3: Verify it's working

Run this in the same terminal:

```bash
curl http://127.0.0.1:7654/health
```

If you see `{"status":"ok"}` — the agent is running and ready. Move on to [Connect from macOS App](#connect-from-macos-app).

#### What each flag does

You don't need to understand these to use the agent, but here's what they do:

| Flag | Plain English |
|---|---|
| `--pid=host` | Lets the agent see all processes on your server, not just its own container |
| `--network=host` | Lets the agent see real network traffic speeds |
| `-v /:/hostfs:ro,rslave` | Gives the agent read-only access to your drives so it can report disk usage |
| `-v /sys:/host/sys:ro` | Gives the agent read-only access to CPU temperature sensors |
| `-v /var/run/docker.sock:...` | Lets the agent see and control your Docker containers |
| `-v /etc/deskmon:/etc/deskmon` | Saves the config file so it survives container updates |
| `DESKMON_HOST_ROOT=/hostfs` | Tells the agent where the host filesystem is mounted |
| `DESKMON_HOST_SYS=/host/sys` | Tells the agent where the host temperature sensors are mounted |
| `--restart unless-stopped` | Auto-starts the agent after server reboots |

> **Is this safe?** Yes. The host filesystem is mounted **read-only** (`ro`). The agent only reads system stats — it cannot modify your files, your array, or your config. The Docker socket is the only read-write access, which allows container start/stop/restart from the macOS app. The agent makes zero outbound connections (no cloud, no telemetry).

#### Alternative: Docker Compose

If you prefer docker compose, run these two commands:

```bash
curl -fsSL -o docker-compose.yml https://raw.githubusercontent.com/neur0map/deskmon-agent/main/docker-compose.yml
docker compose up -d
```

---

### Option B: Prebuilt binary

A single command that downloads the latest release and installs it as a systemd service. No Go or Docker needed.

#### Step 1: Open a terminal on your server

SSH in, or open a terminal directly on the machine.

#### Step 2: Run the installer

```bash
curl -fsSL https://raw.githubusercontent.com/neur0map/deskmon-agent/main/scripts/install-remote.sh | sudo bash
```

This will:
1. Detect your CPU architecture (x86_64 or ARM64)
2. Download the latest release from GitHub
3. Install the binary to `/usr/local/bin/`
4. Create a config file at `/etc/deskmon/config.yaml`
5. Create and start a systemd service

#### Step 3: Verify it's working

```bash
curl http://127.0.0.1:7654/health
```

You should see `{"status":"ok"}`.

**Custom port:**

```bash
curl -fsSL https://raw.githubusercontent.com/neur0map/deskmon-agent/main/scripts/install-remote.sh | sudo bash -s -- --port 9090
```

---

### Option C: Build from source

Requires Go 1.22+ and git installed on the server.

```bash
git clone https://github.com/neur0map/deskmon-agent.git
cd deskmon-agent
sudo make setup
```

One command builds the binary, installs it, creates a systemd service, and starts the agent.

**Custom port:**

```bash
sudo make setup PORT=9090
```

---

### Connect from macOS App

This is the same regardless of which install method you used.

1. Open the Deskmon app on your Mac
2. Go to **Settings** > **+ Add Server**
3. Enter your server's IP address (e.g. `192.168.1.100`)
   - **unRAID users:** This is the same IP you use to access the unRAID web UI. You can find it under **Settings** > **Network Settings** in unRAID, or just look at the address bar when you open the unRAID web UI.
4. Enter your SSH username and password
   - **unRAID users:** Username is `root`. Password is the one you set when you first configured unRAID.
5. The app connects via SSH tunnel, auto-generates an SSH key, and installs it on your server
6. Green dot in the menu bar = connected and receiving data

---

## What You Get

Once running, the macOS app shows live stats for each server:

- **CPU** — Usage %, core count, temperature
- **Memory** — Used / total RAM
- **Disk** — Used / total for each mounted drive (all your unRAID disks, cache, parity)
- **Network** — Download/upload speed (bytes/sec)
- **Uptime** — Time since last boot
- **Docker containers** — Per-container CPU, memory, network, block I/O, PIDs, status

The agent streams live updates via Server-Sent Events (SSE): system stats every 1s, Docker every 5s.

---

## Configuration

### Docker installs

The config file lives at `/etc/deskmon/config.yaml` on your server. It's persisted across container updates via the volume mount.

To change the port:

```bash
# Edit the config
nano /etc/deskmon/config.yaml
```

Change `port: 7654` to your desired port, save, then restart:

```bash
docker restart deskmon-agent
```

### Systemd installs (prebuilt binary / build from source)

Config is at `/etc/deskmon/config.yaml`:

```yaml
port: 7654
bind: "127.0.0.1"
```

To change settings, edit the file and restart:

```bash
sudo nano /etc/deskmon/config.yaml
sudo systemctl restart deskmon-agent
```

Or restart from the macOS app's Settings panel.

---

## Day-to-Day Operations

### Updating the agent

**Docker:**

```bash
docker pull ghcr.io/neur0map/deskmon-agent:latest
docker stop deskmon-agent
docker rm deskmon-agent
```

Then re-run the same `docker run` command from the [Docker install step](#step-2-copy-and-paste-this-entire-command). Your config is preserved (it lives in `/etc/deskmon/`, not inside the container).

If you used docker compose:

```bash
docker compose pull
docker compose up -d
```

**Systemd (prebuilt binary):**

Re-run the install script — it will download the latest version:

```bash
curl -fsSL https://raw.githubusercontent.com/neur0map/deskmon-agent/main/scripts/install-remote.sh | sudo bash
```

**Systemd (built from source):**

```bash
cd deskmon-agent
git pull
sudo make setup
```

### Restarting the agent

**Docker:**

```bash
docker restart deskmon-agent
```

**Systemd:**

```bash
sudo systemctl restart deskmon-agent
```

### Viewing logs

**Docker:**

```bash
docker logs deskmon-agent
docker logs -f deskmon-agent    # follow (live tail)
```

**Systemd:**

```bash
journalctl -u deskmon-agent -f
```

### Stopping / removing the agent

**Docker:**

```bash
docker stop deskmon-agent
docker rm deskmon-agent
```

**Systemd:**

```bash
sudo make uninstall
# or if you used the prebuilt binary:
sudo /usr/local/bin/deskmon-agent --help   # check it exists
sudo systemctl stop deskmon-agent
sudo systemctl disable deskmon-agent
sudo rm /etc/systemd/system/deskmon-agent.service
sudo rm /usr/local/bin/deskmon-agent
sudo rm -rf /etc/deskmon
sudo systemctl daemon-reload
```

---

## Agent Control from macOS

The macOS app can control the agent remotely:

- **Restart Agent** — Sends `POST /agent/restart`. Agent restarts via systemd (~5 seconds). The app auto-reconnects. **Not available in Docker mode** — use `docker restart deskmon-agent` on the server instead.
- **Live connection** — The app connects via SSE (`GET /stats/stream`) for real-time updates. No polling needed.
- **Container management** — Start, stop, restart Docker containers from the app. Works in both Docker and systemd mode.
- **Process management** — Kill processes by PID from the app.

The agent auto-recovers from crashes and starts automatically on server reboot (systemd `Restart=always` or Docker `--restart unless-stopped`).

---

## API Endpoints

The agent binds to `127.0.0.1` only. No authentication is needed — the SSH tunnel handles security.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | `{"status": "ok"}` — online detection |
| `GET` | `/stats` | Full system + Docker container stats |
| `GET` | `/stats/system` | System stats only (no Docker overhead) |
| `GET` | `/stats/docker` | Docker container stats only |
| `GET` | `/stats/processes` | Top processes by CPU |
| `GET` | `/stats/stream` | **SSE stream** — live updates (system 1s, docker 5s) |
| `POST` | `/containers/{id}/start` | Start a Docker container |
| `POST` | `/containers/{id}/stop` | Stop a Docker container |
| `POST` | `/containers/{id}/restart` | Restart a Docker container |
| `POST` | `/processes/{pid}/kill` | Kill a process by PID |
| `POST` | `/agent/restart` | Restart agent via systemd (returns error in Docker mode) |
| `POST` | `/agent/stop` | Stop agent via systemd (returns error in Docker mode) |
| `GET` | `/agent/status` | Agent version and service state |

See [agent-api-contract.md](agent-api-contract.md) for the full JSON schema and field reference.

---

## Security

The agent is hardened as a read-only stats reporter:

- **Localhost only** — Binds to `127.0.0.1`, not reachable from the network. The macOS app connects via SSH tunnel.
- **SSH authentication** — The macOS app authenticates via SSH (password on first connect, then auto-generated ed25519 key). No API tokens.
- **Rate limiting** — 60 requests/minute per IP as secondary defense
- **No injection surface** — Zero user input reaches shell commands, file paths, or system calls. Control endpoints execute hardcoded `systemctl` commands only.
- **Read-only** — Only reads from `/proc`, `/sys`, and Docker socket. No filesystem writes. Docker client is read-only (list and stats only).
- **No outbound connections** — No phoning home, no telemetry, no update checks
- **Systemd sandboxing** — `ProtectSystem=strict`, `ReadOnlyPaths=/`, `ProtectHome=yes`, `NoNewPrivileges=yes` (systemd installs only)
- **Docker: read-only host mount** — Host filesystem is mounted with `ro` (read-only). `privileged` is `false`. The agent cannot modify your files.
- **Config permissions** — `/etc/deskmon/` is root-only (0700), config file is 0600

### No Firewall Needed

The agent only listens on `127.0.0.1:7654`. It is not accessible from the network. The macOS app reaches it through an SSH tunnel, so you only need SSH access (port 22) to your server — no additional firewall rules required.

---

## How Docker Mode Works

> **You don't need to read this section to use the agent.** This is for developers or curious users who want to understand what's happening under the hood.

Docker containers are isolated from the host by default. A normal container can't see the host's CPU usage, real disk sizes, network traffic, or temperature sensors. The agent needs all of that data, so each Docker flag in the install command punches through a specific isolation wall:

| What the agent reads | Problem in Docker | How it's solved |
|---|---|---|
| CPU usage, process list | Container only sees its own processes | `--pid=host` shares the host's process list |
| Network speed | Container has its own virtual network | `--network=host` shares the host's real network |
| Disk usage (all drives) | Container's `/` is a tiny overlay filesystem | `-v /:/hostfs:ro` mounts the real host filesystem (read-only) |
| CPU temperature | Container's `/sys` doesn't have thermal sensors | `-v /sys:/host/sys:ro` mounts the real sysfs (read-only) |
| Docker containers | Socket isn't available inside the container | `-v /var/run/docker.sock:...` passes the Docker socket through |
| RAM, uptime, CPU count | These are kernel-global | Work automatically, no special flags needed |

The two environment variables (`DESKMON_HOST_ROOT`, `DESKMON_HOST_SYS`) tell the agent where to find the mounted host paths. When `DESKMON_HOST_ROOT` is set, the agent:

1. Reads `/proc/1/mounts` (the host's mount list) instead of `/proc/mounts` (the container's mount list)
2. Prefixes mount points with `/hostfs` when checking disk sizes (e.g., checks `/hostfs/mnt/disk1` instead of `/mnt/disk1`)
3. Reports the original mount path in the API (e.g., `/mnt/disk1`, not `/hostfs/mnt/disk1`)

**Not using Docker?** These environment variables are unset by default. The agent reads system paths directly with zero behavior change.

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
│   │   ├── server.go        # HTTP server, rate limiting
│   │   ├── handlers.go      # /health, /stats handlers
│   │   ├── stream.go        # SSE streaming endpoint
│   │   ├── control.go       # /agent/* control handlers
│   │   ├── containers.go    # Container action handlers
│   │   ├── processes.go     # Process kill handler
│   │   └── server_test.go   # API tests
│   ├── collector/
│   │   ├── system.go        # CPU, memory, disk, network, temp, uptime
│   │   ├── docker.go        # Container stats via Docker SDK
│   │   └── broadcast.go     # Generic pub/sub broadcaster for SSE
│   ├── config/
│   │   ├── config.go        # YAML config loader
│   │   └── config_test.go   # Config tests
│   └── systemctl/
│       └── systemctl.go     # Hardcoded systemctl commands (Docker-aware)
├── scripts/
│   ├── install.sh           # Server install/uninstall script
│   └── install-remote.sh    # Curl-based remote installer
├── .github/workflows/
│   └── release.yml          # Build binaries + Docker image on tag push
├── Dockerfile               # Multi-stage build (~15MB image)
├── docker-compose.yml       # Ready-to-use with host access flags
├── Makefile                 # Build, setup, package, test
├── agent-api-contract.md    # API contract (source of truth)
├── ROADMAP.md               # Planned features
└── README.md
```

---

## Troubleshooting

### Docker installs

**Agent not starting / container keeps restarting**

Check the logs:

```bash
docker logs deskmon-agent
```

If you see "permission denied" errors, make sure you're running the docker command as root (or with sudo).

**Container shows as "unhealthy" or "exited"**

```bash
docker ps -a | grep deskmon
```

Look at the STATUS column. If it says "Exited", check logs with `docker logs deskmon-agent`.

**Disks not showing (0 drives in the app)**

This means the agent can't read your host's mount points. Check:

1. The `-v /:/hostfs:ro,rslave` volume mount is present in your docker run command
2. The `-e DESKMON_HOST_ROOT=/hostfs` environment variable is set
3. Try: `docker exec deskmon-agent cat /proc/1/mounts` — you should see your host's drives listed

**Temperature showing 0**

Some systems (VMs, cloud servers, some unRAID configs) don't expose thermal zones. This is normal. Check if your host has them:

```bash
ls /sys/class/thermal/thermal_zone*/temp
```

If that returns "No such file or directory", your system doesn't report temperature and the agent will show 0.

**Restart/stop from macOS app returns an error**

This is expected in Docker mode. The agent can't restart itself because it's inside a container. Use `docker restart deskmon-agent` on the server instead, or recreate the container.

**Containers not showing in the app**

Make sure the Docker socket is mounted:

```bash
docker exec deskmon-agent ls /var/run/docker.sock
```

If that returns an error, re-run the install with the `-v /var/run/docker.sock:/var/run/docker.sock` flag.

### Systemd installs

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
- If Docker is not installed, containers will be an empty array (not an error)

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
