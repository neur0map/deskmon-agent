# Deskmon - Product Plan

Native macOS menu bar app for monitoring home servers.

## Overview

Deskmon consists of two components:

- **Agent** (Go, open source MIT): Runs on servers, collects stats, serves JSON API
- **Client** (SwiftUI, open source): macOS menu bar app, connects to agents

Both components are open source. Revenue comes from selling signed/notarized builds via LemonSqueezy.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         macOS Client                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │  Menu Bar    │  │   Widgets    │  │  Settings    │          │
│  │  - Servers   │  │  - Desktop   │  │  - Alerts    │          │
│  │  - Stats     │  │  - Notif Ctr │  │  - Servers   │          │
│  │  - Alerts    │  │              │  │  - License   │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│                              │                                  │
│                    ┌─────────▼─────────┐                       │
│                    │  Connection Mgr   │                       │
│                    │  - Tailscale IP   │                       │
│                    │  - LAN fallback   │                       │
│                    │  - Auto-reconnect │                       │
│                    └─────────┬─────────┘                       │
└──────────────────────────────┼──────────────────────────────────┘
                               │
                    ┌──────────▼──────────┐
                    │   Network Layer     │
                    │  ┌───────────────┐  │
                    │  │ Tailscale     │  │  ← Remote access
                    │  │ (100.x.x.x)   │  │
                    │  └───────────────┘  │
                    │  ┌───────────────┐  │
                    │  │ LAN           │  │  ← Local access
                    │  │ (192.168.x.x) │  │
                    │  └───────────────┘  │
                    └──────────┬──────────┘
                               │
┌──────────────────────────────┼──────────────────────────────────┐
│                         Linux Server                            │
│                    ┌─────────▼─────────┐                       │
│                    │   Deskmon Agent   │                       │
│                    │   Port 7654       │                       │
│                    └─────────┬─────────┘                       │
│                              │                                  │
│         ┌────────────────────┼────────────────────┐            │
│         ▼                    ▼                    ▼            │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐      │
│  │  gopsutil   │     │  Docker SDK │     │  Optional   │      │
│  │  - CPU      │     │  - Containers│    │  - smartctl │      │
│  │  - Memory   │     │  - Stats    │     │  - sensors  │      │
│  │  - Disk     │     │             │     │  - nvidia   │      │
│  │  - Network  │     │             │     │             │      │
│  └─────────────┘     └─────────────┘     └─────────────┘      │
└─────────────────────────────────────────────────────────────────┘
```

---

## Agent API

**Base URL:** `http://<ip>:7654`

### Endpoints

```
GET /health              Health check
GET /stats               All stats
GET /stats/system        CPU, memory, disk, network, load, uptime, temps
GET /stats/docker        Container stats (if Docker available)
GET /stats/smart         Disk health (if smartctl available)
GET /info                Agent version, capabilities, hostname
```

### Response: GET /stats

```json
{
  "timestamp": "2026-02-11T12:00:00Z",
  "hostname": "homeserver",
  "uptime": 864000,
  "system": {
    "cpu": {
      "percent": 12.5,
      "cores": 4,
      "model": "Intel i5-8250U"
    },
    "memory": {
      "total": 17179869184,
      "used": 8589934592,
      "percent": 50.0,
      "swap_total": 4294967296,
      "swap_used": 0
    },
    "disk": [
      {
        "mount": "/",
        "total": 500000000000,
        "used": 250000000000,
        "percent": 50.0
      }
    ],
    "network": {
      "bytes_sent": 1000000000,
      "bytes_recv": 5000000000
    },
    "load": {
      "load1": 0.5,
      "load5": 0.4,
      "load15": 0.3
    },
    "temps": [
      {"label": "CPU", "current": 45.0}
    ]
  },
  "docker": {
    "containers": [
      {
        "id": "abc123",
        "name": "pihole",
        "status": "running",
        "cpu_percent": 2.5,
        "memory_used": 134217728,
        "memory_limit": 536870912
      }
    ]
  }
}
```

---

## Remote Access Strategy

### Connection Priority

1. **Tailscale IP** (100.x.x.x) - Works from anywhere
2. **LAN IP** (192.168.x.x) - Works on same network
3. **mDNS/Bonjour** - Discovery fallback

### Tailscale Integration

Agent detects Tailscale:

```
$ deskmon-agent

Deskmon Agent v0.1.0
────────────────────────────────

Network:
  LAN IP:        192.168.1.50
  Tailscale:     ✓ Connected (100.64.1.5)
  Tailnet:       user@github

Listening on:
  http://192.168.1.50:7654   (LAN)
  http://100.64.1.5:7654     (Tailscale)

Pair with macOS app: ABCD-1234
```

If Tailscale not installed:

```
$ deskmon-agent

Network:
  LAN IP:        192.168.1.50
  Tailscale:     ✗ Not installed

⚠️  Remote access unavailable.

Install Tailscale for access from anywhere:
  curl -fsSL https://tailscale.com/install.sh | sh
  sudo tailscale up

Listening on:
  http://192.168.1.50:7654   (LAN only)
```

### macOS Client Detection

```
┌─────────────────────────────────────────────┐
│  ⚠️ Tailscale not detected                  │
│                                             │
│  For remote access outside your network,    │
│  install Tailscale on this Mac and server.  │
│                                             │
│  [Get Tailscale]  [Continue LAN only]       │
└─────────────────────────────────────────────┘
```

---

## Agent Architecture

### Collector Pattern

```go
type Collector interface {
    Name() string
    Available() bool
    Collect() (interface{}, error)
}

// Always available (gopsutil)
collectors := []Collector{
    NewCPUCollector(),
    NewMemoryCollector(),
    NewDiskCollector(),
    NewNetworkCollector(),
    NewLoadCollector(),
    NewUptimeCollector(),
}

// Conditionally available
if dockerAvailable() {
    collectors = append(collectors, NewDockerCollector())
}
if smartctlAvailable() {
    collectors = append(collectors, NewSmartCollector())
}
if sensorsAvailable() {
    collectors = append(collectors, NewTempCollector())
}
if nvidiaSmiAvailable() {
    collectors = append(collectors, NewNvidiaCollector())
}
```

### Core Dependencies

| Dependency | Purpose | Install |
|------------|---------|---------|
| `gopsutil` | CPU, memory, disk, network, load | Go library (bundled) |
| Docker SDK | Container stats | Go library (bundled) |

### Optional Tool Integrations

| Tool | Purpose | Detection |
|------|---------|-----------|
| `smartctl` | Disk SMART health | `which smartctl` |
| `lm-sensors` | Hardware temps | `which sensors` |
| `nvidia-smi` | Nvidia GPU stats | `which nvidia-smi` |
| `nvme-cli` | NVMe health | `which nvme` |
| `zfsutils` | ZFS pool status | `which zpool` |

### Capability Reporting

```json
{
  "capabilities": {
    "cpu": true,
    "memory": true,
    "disk": true,
    "network": true,
    "docker": true,
    "smart": true,
    "temps": false,
    "nvidia": false,
    "zfs": false
  },
  "suggestions": [
    "Install lm-sensors for temperature monitoring: apt install lm-sensors",
    "Install smartmontools for disk health: apt install smartmontools"
  ]
}
```

---

## macOS Client Features

### Free Tier

- 1 server
- Basic stats (CPU, RAM, disk, network)
- 60-second refresh
- Menu bar display
- LAN connectivity

### Pro Tier ($19 one-time)

- Unlimited servers
- 5-second refresh
- Docker container stats
- Push notifications (threshold alerts)
- Historical sparklines (last hour)
- macOS widgets
- Quick actions (container restart)
- Tailscale auto-detection

### Menu Bar Display

```
┌──────────────────────────────────┐
│ homeserver          ● Connected  │
├──────────────────────────────────┤
│ CPU     ▃▅▂▄▃▂▅▃   12%          │
│ Memory  ████████░░  8.0/16 GB    │
│ Disk    █████░░░░░  234/500 GB   │
│ Network ↑ 1.2 MB/s  ↓ 5.4 MB/s   │
├──────────────────────────────────┤
│ 🐳 Containers (8 running)        │
│   pihole        ●  2% / 128 MB   │
│   plex          ●  5% / 2.1 GB   │
│   homeassistant ●  3% / 512 MB   │
│   [Show all...]                  │
├──────────────────────────────────┤
│ Settings...                      │
│ Add Server...                    │
│ Quit Deskmon                     │
└──────────────────────────────────┘
```

---

## Business Model

### Pricing

| Option | Price | What You Get |
|--------|-------|--------------|
| **Build from source** | Free | Full app, unsigned, no updates |
| **Signed build** | $19 one-time | Notarized DMG, auto-updates |

### Why Pay?

Building from source requires:
- Xcode (12GB download)
- Change signing team
- Gatekeeper warnings on every launch
- No automatic updates

$19 gets:
- Signed, notarized DMG
- Automatic updates via Sparkle
- Support the developer

### Distribution

```
LemonSqueezy checkout
        ↓
Payment success
        ↓
Email with download link
        ↓
User downloads signed DMG
```

### README Installation Section

```markdown
## Installation

### Option A: Download (Recommended)
Get the signed build for $19 → [deskmon.dev](https://deskmon.dev)
- Notarized, no Gatekeeper warnings
- Automatic updates

### Option B: Build from source
1. Clone this repo
2. Open `Deskmon.xcodeproj` in Xcode 15+
3. Change signing team to your Apple ID
4. Build and run

Note: Self-built versions are unsigned and won't receive 
automatic updates.
```

---

## Repositories

| Repo | Content | License |
|------|---------|---------|
| `github.com/neur0map/deskmon-agent` | Go agent | MIT |
| `github.com/neur0map/deskmon` | macOS app | MIT |

---

## Development Phases

### Phase 1: Core Agent (Week 1-2)

- [ ] Project scaffold with Go modules
- [ ] gopsutil collectors (CPU, memory, disk, network, load)
- [ ] HTTP server with /health, /stats endpoints
- [ ] Docker integration via socket
- [ ] Tailscale detection
- [ ] Pairing code generation
- [ ] Install script (curl | bash)

### Phase 2: Core Client (Week 3-4)

- [ ] SwiftUI menu bar app scaffold
- [ ] Server connection manager
- [ ] Basic stats display
- [ ] Pairing flow (manual code entry)
- [ ] Tailscale detection
- [ ] Settings persistence

### Phase 3: Polish (Week 5-6)

- [ ] Historical sparklines
- [ ] Docker container list
- [ ] Refresh rate settings
- [ ] Notification system
- [ ] Multi-server support
- [ ] Error handling and reconnection

### Phase 4: Pro Features (Week 7-8)

- [ ] LemonSqueezy integration
- [ ] License validation
- [ ] macOS widgets
- [ ] Push notifications
- [ ] Quick actions (container restart)
- [ ] Sparkle auto-updater

### Phase 5: Launch

- [ ] Landing page (deskmon.dev)
- [ ] Documentation
- [ ] Demo video
- [ ] r/homelab launch post
- [ ] ProductHunt launch

---

## Competitor Analysis

### Beszel

- Lightweight, web-based
- Hub + Agent architecture
- Docker stats
- SMART, GPU, temps
- 4.6k GitHub stars

**Deskmon advantage:** Native macOS, menu bar, widgets, no web dashboard

### Uptime Kuma

- Focused on uptime monitoring, not server stats
- SQLite bloat issues over time
- Web-based

**Deskmon advantage:** System stats focus, native app, no database

### Grafana + Prometheus

- Powerful but complex
- Overkill for small setups
- Steep learning curve

**Deskmon advantage:** Zero config, works out of box

### iStatMenus

- Native macOS, excellent
- Local machine only
- No remote server monitoring

**Deskmon advantage:** Remote servers, Docker, multi-server

---

## Target Users

- Homelab enthusiasts
- Self-hosters
- Developers with home servers
- Small business with on-prem servers
- Anyone who runs `htop` over SSH

---

## Key Differentiators

1. **Native macOS** - Not another web dashboard
2. **Menu bar** - One click, always visible
3. **Zero config** - Install agent, pair, done
4. **Tailscale integration** - Remote access without port forwarding
5. **Lightweight** - Agent uses minimal resources
6. **Open source** - Full transparency, community contributions
7. **One-time payment** - No subscriptions for a utility app

---

## Links

- **Domains:** deskmon.dev, deskmon.app
- **Agent repo:** github.com/neur0map/deskmon-agent
- **Client repo:** github.com/neur0map/deskmon
