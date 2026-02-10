# deskmon-agent

Lightweight system monitoring agent for Linux servers. Collects system stats and exposes them via a simple HTTP API.

Designed to work with [Deskmon](https://deskmon.dev) for macOS, but the API is open and can be used with any client.

![License](https://img.shields.io/badge/license-MIT-green)
![Platform](https://img.shields.io/badge/platform-Linux-blue)

---

## Features

- **Lightweight**: Single static binary, minimal resource usage
- **Zero config**: Works out of the box with sensible defaults
- **Simple API**: JSON over HTTP, easy to integrate
- **Docker stats**: Monitor container CPU, memory, and status
- **Extensible**: Plugin system for app integrations (Pihole, Plex, etc.)

---

## Quick Start

### Install

```bash
curl -fsSL https://deskmon.dev/install.sh | bash
```

Or download from [releases](https://github.com/neur0map/deskmon-agent/releases).

### Run

```bash
# Run in foreground
deskmon-agent

# Run as systemd service
sudo systemctl enable --now deskmon-agent
```

### Verify

```bash
curl http://localhost:7654/health
# {"status":"ok","version":"0.1.0"}

curl http://localhost:7654/stats
# Full system stats JSON
```

---

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check, returns version |
| `GET /stats` | All available stats |
| `GET /stats/system` | CPU, memory, disk, network, load |
| `GET /stats/docker` | Container list with stats |
| `GET /stats/integrations` | App-specific stats (if configured) |

### Example Response: `/stats`

```json
{
  "hostname": "homeserver",
  "uptime": 1234567,
  "cpu": {
    "usage": 23.5,
    "cores": 4,
    "load": [1.2, 0.8, 0.6]
  },
  "memory": {
    "used": 4294967296,
    "total": 17179869184,
    "swap_used": 0,
    "swap_total": 8589934592
  },
  "disks": [
    {
      "mount": "/",
      "used": 128849018880,
      "total": 512110190592,
      "read_bytes": 1234567,
      "write_bytes": 7654321
    }
  ],
  "network": {
    "rx_bytes": 12345678900,
    "tx_bytes": 9876543210,
    "rx_rate": 1234567,
    "tx_rate": 456789
  },
  "temperature": {
    "cpu": 45.0
  },
  "containers": [
    {
      "id": "abc123",
      "name": "pihole",
      "image": "pihole/pihole:latest",
      "status": "running",
      "cpu": 0.5,
      "memory": 134217728,
      "ports": ["53/tcp", "80/tcp"]
    }
  ]
}
```

---

## Configuration

Configuration is optional. Create `/etc/deskmon/config.yaml` to customize:

```yaml
# Server settings
port: 7654
bind: "0.0.0.0"

# Optional auth token (recommended for non-local access)
auth_token: "your-secret-token"

# Docker socket path (auto-detected if not set)
docker_socket: "/var/run/docker.sock"

# Integrations
integrations:
  pihole:
    enabled: true
    url: "http://localhost:80"
    api_key: "your-pihole-api-key"
  
  plex:
    enabled: false
    url: "http://localhost:32400"
    token: "your-plex-token"
```

---

## Integrations

### Pihole

```yaml
integrations:
  pihole:
    enabled: true
    url: "http://localhost:80"
    api_key: "your-api-key"  # From Pihole admin > Settings > API
```

Returns:
```json
{
  "pihole": {
    "status": "enabled",
    "queries_today": 45621,
    "blocked_today": 12453,
    "block_percent": 27.3,
    "gravity_size": 892341
  }
}
```

### Plex (coming soon)

### Jellyfin (coming soon)

### Home Assistant (coming soon)

---

## Security

By default, deskmon-agent binds to all interfaces (`0.0.0.0`). For security:

1. **Firewall**: Only allow access from your LAN or specific IPs
2. **Auth token**: Set `auth_token` in config, pass via `Authorization: Bearer <token>` header
3. **Bind locally**: Set `bind: "127.0.0.1"` and use SSH tunnel or VPN

---

## Building from Source

```bash
git clone https://github.com/neur0map/deskmon-agent.git
cd deskmon-agent
make build

# Cross-compile for Linux
make build-linux-amd64
make build-linux-arm64
```

Requirements: Go 1.21+

---

## Systemd Service

The install script creates this automatically, but for manual setup:

```ini
# /etc/systemd/system/deskmon-agent.service
[Unit]
Description=Deskmon Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/deskmon-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now deskmon-agent
```

---

## Troubleshooting

**Agent not starting**
- Check logs: `journalctl -u deskmon-agent -f`
- Verify port not in use: `ss -tlnp | grep 7654`

**Docker stats not showing**
- Ensure user is in docker group: `sudo usermod -aG docker $USER`
- Or run agent as root (not recommended)

**Permission denied on /sys files**
- Some temperature sensors require root access
- Agent will skip unavailable metrics gracefully

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

## Related

- [Deskmon](https://deskmon.dev) - macOS menu bar app (closed source)
- [Deskmon Documentation](https://docs.deskmon.dev) - Full documentation
