package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func init() {
	Register(&PiHolePlugin{})
}

// PiHolePlugin detects and collects stats from Pi-hole (v5 and v6).
type PiHolePlugin struct{}

func (p *PiHolePlugin) ID() string   { return "pihole" }
func (p *PiHolePlugin) Name() string { return "Pi-hole" }
func (p *PiHolePlugin) Icon() string { return "shield.checkerboard" }

// --- Pi-hole v6 session management ---

type piholeSession struct {
	mu        sync.Mutex
	sid       string
	csrf      string
	expiresAt time.Time
}

var v6Session = &piholeSession{}

func (s *piholeSession) isValid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sid != "" && time.Now().Before(s.expiresAt)
}

func (s *piholeSession) get() (sid, csrf string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sid, s.csrf
}

func (s *piholeSession) set(sid, csrf string, validity int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sid = sid
	s.csrf = csrf
	// Expire slightly early to avoid edge cases
	if validity <= 0 {
		validity = 300
	}
	s.expiresAt = time.Now().Add(time.Duration(validity-10) * time.Second)
	log.Printf("services: pihole v6 session acquired (expires in %ds)", validity)
}

func (s *piholeSession) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sid = ""
	s.csrf = ""
	s.expiresAt = time.Time{}
}

// authenticate performs POST /api/auth with the given password and caches the session.
func (s *piholeSession) authenticate(ctx context.Context, baseURL, password string) error {
	payload, _ := json.Marshal(map[string]string{"password": password})

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/auth", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	cl := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}

	if resp.StatusCode == 401 {
		return fmt.Errorf("invalid Pi-hole password")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("auth returned HTTP %d", resp.StatusCode)
	}

	// Parse: {"session":{"valid":true,"sid":"...","csrf":"...","validity":300}}
	var authResp struct {
		Session struct {
			Valid    bool   `json:"valid"`
			SID      string `json:"sid"`
			CSRF     string `json:"csrf"`
			Validity int    `json:"validity"`
		} `json:"session"`
	}
	if err := json.Unmarshal(body, &authResp); err != nil {
		return fmt.Errorf("parse auth response: %w", err)
	}

	if !authResp.Session.Valid {
		return fmt.Errorf("Pi-hole auth returned valid=false")
	}

	s.set(authResp.Session.SID, authResp.Session.CSRF, authResp.Session.Validity)
	return nil
}

// httpGetWithSID performs a GET with X-FTL-SID and X-FTL-CSRF headers.
func httpGetWithSID(ctx context.Context, url, sid, csrf string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	if sid != "" {
		req.Header.Set("X-FTL-SID", sid)
		req.Header.Set("X-FTL-CSRF", csrf)
	}

	cl := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := cl.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}

// --- Detection ---

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
	if env.HasProcess("pihole-FTL") || env.HasProcessSubstring("pihole") {
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

		if cfgPort := readPiHoleConfigPort(); cfgPort > 0 {
			log.Printf("services: pihole config says port %d", cfgPort)
			if url := p.probeAPI(env, []int{cfgPort}); url != "" {
				base.BaseURL = url
				log.Printf("services: pihole detected via config at %s", url)
				return base
			}
		}

		if url := p.probeAPI(env, []int{80, 8080, 443, 8443}); url != "" {
			base.BaseURL = url
			log.Printf("services: pihole detected via common ports at %s", url)
			return base
		}

		log.Printf("services: pihole process found but no API reachable")
	}

	return nil
}

func (p *PiHolePlugin) probeAPI(env *DetectionEnv, ports []int) string {
	if url := env.ProbeHTTP(ports, "/admin/api.php?summaryRaw"); url != "" {
		return url
	}
	if url := env.ProbeHTTP(ports, "/api/dns/blocking"); url != "" {
		return url
	}
	if url := env.ProbeHTTP(ports, "/api/auth"); url != "" {
		return url
	}
	if url := env.ProbeHTTP(ports, "/admin/"); url != "" {
		return url
	}
	return ""
}

func readPiHoleConfigPort() int {
	if data, err := os.ReadFile("/etc/pihole/pihole.toml"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "port") && strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, "\"'")
				portStr := strings.Split(val, ",")[0]
				portStr = strings.TrimSuffix(portStr, "s")
				if p, err := strconv.Atoi(strings.TrimSpace(portStr)); err == nil && p > 0 {
					return p
				}
			}
		}
	}

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

// --- Collection ---

func (p *PiHolePlugin) Collect(ctx context.Context, svc *DetectedService) (*ServiceStats, error) {
	stats := &ServiceStats{
		PluginID: p.ID(),
		Name:     p.Name(),
		Icon:     p.Icon(),
		Status:   "running",
		Stats:    make(map[string]interface{}),
	}

	password := svc.Meta["password"]

	// Try v5 API first
	v5err := ""
	if data, err := p.collectV5(ctx, svc.BaseURL); err == nil {
		stats.Summary = data.summary
		stats.Stats = data.stats
		stats.Status = data.status
		if svc.Version == "" {
			svc.Version = "v5"
		}
		return stats, nil
	} else {
		v5err = err.Error()
	}

	// Try v6 API (with authentication if password is available)
	v6err := ""
	if data, err := p.collectV6Authenticated(ctx, svc.BaseURL, password); err == nil {
		stats.Summary = data.summary
		stats.Stats = data.stats
		stats.Status = data.status
		if svc.Version == "" {
			svc.Version = "v6"
		}
		return stats, nil
	} else {
		v6err = err.Error()
	}

	// Try v6 public endpoint (no auth required)
	if data, err := p.collectV6Public(ctx, svc.BaseURL); err == nil {
		stats.Summary = data.summary
		stats.Stats = data.stats
		stats.Status = data.status
		if svc.Version == "" {
			svc.Version = "v6"
		}
		return stats, nil
	}

	// If we got 401 from v6 and have no password, signal auth required
	if strings.Contains(v6err, "401") || strings.Contains(v6err, "authentication") {
		if svc.Version == "" {
			svc.Version = "v6"
		}
		stats.Status = "running"
		stats.Summary = []StatItem{
			{Label: "Version", Value: "v6", Type: "text"},
			{Label: "Status", Value: "Running", Type: "status"},
		}
		stats.Stats = map[string]interface{}{
			"version":      "v6",
			"authRequired": true,
		}
		return stats, nil
	}

	return nil, fmt.Errorf("could not reach Pi-hole API at %s (v5: %s, v6: %s)", svc.BaseURL, v5err, v6err)
}

type piholeData struct {
	summary []StatItem
	stats   map[string]interface{}
	status  string
}

func (p *PiHolePlugin) collectV5(ctx context.Context, baseURL string) (*piholeData, error) {
	body, err := HTTPGet(ctx, baseURL+"/admin/api.php?summaryRaw")
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

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

// collectV6Authenticated tries to fetch v6 stats, authenticating if needed.
func (p *PiHolePlugin) collectV6Authenticated(ctx context.Context, baseURL, password string) (*piholeData, error) {
	// If we have a valid session, try it first
	if v6Session.isValid() {
		sid, csrf := v6Session.get()
		data, err := p.fetchV6Summary(ctx, baseURL, sid, csrf)
		if err == nil {
			return data, nil
		}
		// Session expired or invalid — clear and retry
		log.Printf("services: pihole v6 session expired, re-authenticating")
		v6Session.clear()
	}

	// Try without auth (Pi-hole might not require a password)
	data, err := p.fetchV6Summary(ctx, baseURL, "", "")
	if err == nil {
		return data, nil
	}

	// Need auth — authenticate with password
	if password == "" {
		return nil, fmt.Errorf("Pi-hole v6 requires authentication (401)")
	}

	if err := v6Session.authenticate(ctx, baseURL, password); err != nil {
		return nil, fmt.Errorf("pihole auth: %w", err)
	}

	// Retry with new session
	sid, csrf := v6Session.get()
	return p.fetchV6Summary(ctx, baseURL, sid, csrf)
}

// fetchV6Summary fetches /api/stats/summary with optional SID auth.
func (p *PiHolePlugin) fetchV6Summary(ctx context.Context, baseURL, sid, csrf string) (*piholeData, error) {
	body, statusCode, err := httpGetWithSID(ctx, baseURL+"/api/stats/summary", sid, csrf)
	if err != nil {
		return nil, err
	}
	if statusCode == 401 {
		return nil, fmt.Errorf("HTTP 401")
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", statusCode)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

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

	return &piholeData{
		status: "running",
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
			"status":          "running",
			"version":         "v6",
		},
	}, nil
}

func (p *PiHolePlugin) collectV6Public(ctx context.Context, baseURL string) (*piholeData, error) {
	body, err := HTTPGet(ctx, baseURL+"/api/dns/blocking")
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	blocking, _ := raw["blocking"].(string)
	piStatus := "running"
	if blocking == "disabled" {
		piStatus = "stopped"
	}

	return &piholeData{
		status: piStatus,
		summary: []StatItem{
			{Label: "DNS Blocking", Value: blocking, Type: "status"},
		},
		stats: map[string]interface{}{
			"blocking": blocking,
			"status":   piStatus,
			"version":  "v6",
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
