package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
)

func init() {
	Register(&PiHolePlugin{})
}

// PiHolePlugin detects and collects stats from Pi-hole (v5 and v6).
type PiHolePlugin struct{}

func (p *PiHolePlugin) ID() string   { return "pihole" }
func (p *PiHolePlugin) Name() string { return "Pi-hole" }
func (p *PiHolePlugin) Icon() string { return "shield.checkerboard" }

func (p *PiHolePlugin) Detect(ctx context.Context, env *DetectionEnv) *DetectedService {
	base := &DetectedService{
		PluginID: p.ID(),
		Name:     p.Name(),
		Icon:     p.Icon(),
		Meta:     make(map[string]string),
	}

	// Strategy 1: Docker container with "pihole" in image name
	if c := env.FindDockerImage("pihole"); c != nil && c.State == "running" {
		ports := append(c.HostPorts, 80, 8080)
		if url := p.probeAPI(env, ports); url != "" {
			base.BaseURL = url
			log.Printf("services: pihole detected via docker (%s) at %s", c.Image, url)
			return base
		}
	}

	// Strategy 2: pihole-FTL process running (bare metal install)
	// Use substring match — comm name might be "pihole-FTL", "pihole", etc.
	if env.HasProcess("pihole-FTL") || env.HasProcessSubstring("pihole") {
		// Use actual listening ports discovered from /proc/net/tcp
		ports := env.FindProcessPorts("pihole-FTL")
		if len(ports) == 0 {
			ports = env.FindProcessPortsBySubstring("pihole")
		}
		if len(ports) > 0 {
			log.Printf("services: pihole process listening on ports %v", ports)
			if url := p.probeAPI(env, ports); url != "" {
				base.BaseURL = url
				log.Printf("services: pihole detected via process ports at %s", url)
				return base
			}
		}

		// Fallback: read Pi-hole config files for the web port
		if cfgPort := readPiHoleConfigPort(); cfgPort > 0 {
			log.Printf("services: pihole config says port %d", cfgPort)
			if url := p.probeAPI(env, []int{cfgPort}); url != "" {
				base.BaseURL = url
				log.Printf("services: pihole detected via config at %s", url)
				return base
			}
		}

		// Last resort: common ports
		if url := p.probeAPI(env, []int{80, 8080, 443, 8443}); url != "" {
			base.BaseURL = url
			log.Printf("services: pihole detected via common ports at %s", url)
			return base
		}

		log.Printf("services: pihole process found but no API reachable")
	}

	// No blind probe — too many false positives without process confirmation
	return nil
}

// probeAPI tries Pi-hole v5 and v6 API endpoints on the given ports.
func (p *PiHolePlugin) probeAPI(env *DetectionEnv, ports []int) string {
	// Try v5 API endpoint
	if url := env.ProbeHTTP(ports, "/admin/api.php?summaryRaw"); url != "" {
		return url
	}
	// Try v6 API endpoint
	if url := env.ProbeHTTP(ports, "/api/info"); url != "" {
		return url
	}
	return ""
}

// readPiHoleConfigPort reads Pi-hole config files to find the web interface port.
func readPiHoleConfigPort() int {
	// Pi-hole v6: /etc/pihole/pihole.toml — "port = "80,443s""
	if data, err := os.ReadFile("/etc/pihole/pihole.toml"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "port") && strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, "\"'")
				// port can be "80,443s" — take first number
				portStr := strings.Split(val, ",")[0]
				portStr = strings.TrimSuffix(portStr, "s")
				if p, err := strconv.Atoi(strings.TrimSpace(portStr)); err == nil && p > 0 {
					return p
				}
			}
		}
	}

	// Pi-hole v5: lighttpd config — "server.port = 80"
	for _, path := range []string{"/etc/lighttpd/external.conf", "/etc/lighttpd/lighttpd.conf"} {
		if data, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "server.port") {
					parts := strings.SplitN(line, "=", 2)
					val := strings.TrimSpace(parts[1])
					if p, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && p > 0 {
						return p
					}
				}
			}
		}
	}

	return 0
}

func (p *PiHolePlugin) Collect(ctx context.Context, svc *DetectedService) (*ServiceStats, error) {
	stats := &ServiceStats{
		PluginID: p.ID(),
		Name:     p.Name(),
		Icon:     p.Icon(),
		Status:   "running",
		Stats:    make(map[string]interface{}),
	}

	// Try v5 API first
	if data, err := p.collectV5(ctx, svc.BaseURL); err == nil {
		stats.Summary = data.summary
		stats.Stats = data.stats
		stats.Status = data.status
		if svc.Version == "" {
			svc.Version = "v5"
		}
		return stats, nil
	}

	// Try v6 API
	if data, err := p.collectV6(ctx, svc.BaseURL); err == nil {
		stats.Summary = data.summary
		stats.Stats = data.stats
		stats.Status = data.status
		if svc.Version == "" {
			svc.Version = "v6"
		}
		return stats, nil
	}

	return nil, fmt.Errorf("could not reach Pi-hole API at %s", svc.BaseURL)
}

type piholeData struct {
	summary []StatItem
	stats   map[string]interface{}
	status  string
}

// collectV5 fetches stats from Pi-hole v5's /admin/api.php?summaryRaw endpoint.
func (p *PiHolePlugin) collectV5(ctx context.Context, baseURL string) (*piholeData, error) {
	body, err := HTTPGet(ctx, baseURL+"/admin/api.php?summaryRaw")
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	// Validate this is actually a Pi-hole response
	if _, ok := raw["dns_queries_today"]; !ok {
		return nil, fmt.Errorf("not a Pi-hole response")
	}

	queriesToday := toInt64(raw["dns_queries_today"])
	adsBlocked := toInt64(raw["ads_blocked_today"])
	adsPercent := toFloat64(raw["ads_percentage_today"])
	domainsBlocked := toInt64(raw["domains_being_blocked"])
	uniqueClients := toInt64(raw["unique_clients"])

	piStatus := "running"
	if s, ok := raw["status"].(string); ok && s != "enabled" {
		piStatus = "stopped"
	}

	return &piholeData{
		status: piStatus,
		summary: []StatItem{
			{Label: "Queries Today", Value: FormatNumber(queriesToday), Type: "number"},
			{Label: "Blocked", Value: fmt.Sprintf("%.1f%%", adsPercent), Type: "percent"},
			{Label: "On Blocklist", Value: FormatNumber(domainsBlocked), Type: "number"},
			{Label: "Clients", Value: FormatNumber(uniqueClients), Type: "number"},
		},
		stats: map[string]interface{}{
			"queriesToday":     queriesToday,
			"adsBlockedToday":  adsBlocked,
			"adsPercentToday":  math.Round(adsPercent*10) / 10,
			"domainsBlocked":   domainsBlocked,
			"uniqueClients":    uniqueClients,
			"queriesForwarded": toInt64(raw["queries_forwarded"]),
			"queriesCached":    toInt64(raw["queries_cached"]),
			"uniqueDomains":    toInt64(raw["unique_domains"]),
			"status":           raw["status"],
			"version":          "v5",
		},
	}, nil
}

// collectV6 fetches stats from Pi-hole v6's /api/stats/summary endpoint.
func (p *PiHolePlugin) collectV6(ctx context.Context, baseURL string) (*piholeData, error) {
	body, err := HTTPGet(ctx, baseURL+"/api/stats/summary")
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	// v6 structure differs from v5
	queries := extractNested(raw, "queries")
	if queries == nil {
		return nil, fmt.Errorf("not a Pi-hole v6 response")
	}

	totalQueries := toInt64(queries["total"])
	blockedQueries := toInt64(queries["blocked"])
	var blockedPercent float64
	if totalQueries > 0 {
		blockedPercent = float64(blockedQueries) / float64(totalQueries) * 100
	}

	gravity := extractNested(raw, "gravity")
	domainsBlocked := toInt64(gravity["domains_being_blocked"])

	clients := extractNested(raw, "clients")
	activeClients := toInt64(clients["active"])

	piStatus := "running"

	return &piholeData{
		status: piStatus,
		summary: []StatItem{
			{Label: "Queries Today", Value: FormatNumber(totalQueries), Type: "number"},
			{Label: "Blocked", Value: fmt.Sprintf("%.1f%%", blockedPercent), Type: "percent"},
			{Label: "On Blocklist", Value: FormatNumber(domainsBlocked), Type: "number"},
			{Label: "Clients", Value: FormatNumber(activeClients), Type: "number"},
		},
		stats: map[string]interface{}{
			"queriesToday":    totalQueries,
			"adsBlockedToday": blockedQueries,
			"adsPercentToday": math.Round(blockedPercent*10) / 10,
			"domainsBlocked":  domainsBlocked,
			"activeClients":   activeClients,
			"status":          piStatus,
			"version":         "v6",
		},
	}, nil
}

// --- helpers ---

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		// Pi-hole sometimes returns numbers as strings with commas
		clean := strings.ReplaceAll(n, ",", "")
		var i int64
		fmt.Sscanf(clean, "%d", &i)
		return i
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		fmt.Sscanf(n, "%f", &f)
		return f
	}
	return 0
}

func extractNested(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		if nested, ok := v.(map[string]interface{}); ok {
			return nested
		}
	}
	return nil
}
