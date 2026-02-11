# Roadmap

Current state: **MVP** -- core system stats, Docker monitoring, service detection, SSE streaming, and remote agent control are working.

---

## Completed

- System metrics: CPU, memory, disk, network speed, uptime, temperature
- Docker container stats: CPU, memory, network, block I/O, PIDs, ports, health status
- Container management: start, stop, restart from macOS app
- Process monitoring: top 10 by CPU with EMA smoothing
- Process control: kill by PID
- Service auto-detection: Pi-hole (v5/v6), Traefik, Nginx
- Service configuration: set credentials via API (persisted to config)
- SSE streaming: system (1s), Docker (5s), services (10s)
- Agent control: restart/stop via systemd from macOS app
- Bearer token auth with constant-time comparison
- Rate limiting (60 req/min per IP)
- Security hardening: systemd sandboxing, read-only filesystem, no shell injection surface
- One-command install: `sudo make setup`
- Cross-compilation for Linux amd64 and arm64

---

## Short Term

- **SMART disk health** -- Read disk health via smartctl when available. Report drive temperature, hours, reallocated sectors.
- **GPU stats** -- Nvidia GPU monitoring via nvidia-smi (utilization, temperature, memory, fan speed).
- **ZFS pool status** -- Pool health, capacity, scrub status for ZFS users.
- **Multiple disk mounts** -- Report stats for all mounted filesystems, not just root.
- **Per-core CPU** -- Break down CPU usage per core for multi-core visibility.
- **Load averages** -- Expose 1/5/15 minute load averages (data is already available in /proc).
- **Swap usage** -- Report swap used/total alongside memory stats.

---

## Medium Term

- **Service plugins** -- Add detection for more self-hosted services: Plex, Home Assistant, Nextcloud, Jellyfin, Portainer, Grafana, Prometheus.
- **Custom alert thresholds** -- Configurable CPU/memory/disk thresholds that trigger push notifications on the macOS app.
- **Historical data** -- Keep last-hour sparkline data in memory for the macOS app to render trends.
- **mDNS/Bonjour discovery** -- Auto-discover agents on the local network so users don't have to enter IPs manually.
- **Config hot reload** -- Watch config file for changes and apply without restart.
- **Structured logging** -- JSON log output option for log aggregation tools.

---

## Long Term

- **Tailscale integration** -- Detect Tailscale, report tailnet status, advertise Tailscale IP alongside LAN IP.
- **Multi-user auth** -- Support multiple tokens with different permission levels (read-only vs full control).
- **TLS support** -- Optional HTTPS with self-signed or user-provided certificates.
- **Update mechanism** -- Agent self-update from GitHub releases.
- **Webhook notifications** -- Push alerts to Slack, Discord, or generic webhooks when thresholds are crossed.
- **Plugin SDK** -- Documented interface for community-contributed service plugins.
