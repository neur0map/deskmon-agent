package services

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	Register(&NginxPlugin{})
}

// NginxPlugin detects and collects stats from Nginx's stub_status module.
type NginxPlugin struct{}

func (p *NginxPlugin) ID() string   { return "nginx" }
func (p *NginxPlugin) Name() string { return "Nginx" }
func (p *NginxPlugin) Icon() string { return "globe" }

// Common stub_status paths to try during detection.
var nginxStatusPaths = []string{"/nginx_status", "/stub_status", "/status", "/basic_status"}

func (p *NginxPlugin) Detect(ctx context.Context, env *DetectionEnv) *DetectedService {
	base := &DetectedService{
		PluginID: p.ID(),
		Name:     p.Name(),
		Icon:     p.Icon(),
		Meta:     make(map[string]string),
	}

	// Strategy 1: Docker container with "nginx" in image name (exclude traefik)
	if c := env.FindDockerImage("nginx"); c != nil && c.State == "running" {
		if !strings.Contains(strings.ToLower(c.Image), "traefik") {
			ports := append(c.HostPorts, 80, 8080)
			if url, path := probeNginxStatus(env, ports); url != "" {
				base.BaseURL = url
				base.Meta["statusPath"] = path
				log.Printf("services: nginx detected via docker (%s) at %s%s", c.Image, url, path)
				return base
			}
		}
	}

	// Strategy 2: nginx process running
	if env.HasProcess("nginx") {
		ports := env.FindProcessPorts("nginx")
		ports = append(ports, 80, 8080, 443, 8443)
		if url, path := probeNginxStatus(env, ports); url != "" {
			base.BaseURL = url
			base.Meta["statusPath"] = path
			log.Printf("services: nginx detected via process at %s%s", url, path)
			return base
		}
	}

	return nil
}

// probeNginxStatus tries each status path on each port, returns (baseURL, path) or ("", "").
func probeNginxStatus(env *DetectionEnv, ports []int) (string, string) {
	for _, path := range nginxStatusPaths {
		if url := env.ProbeHTTP(ports, path); url != "" {
			return url, path
		}
	}
	return "", ""
}

func (p *NginxPlugin) Collect(ctx context.Context, svc *DetectedService) (*ServiceStats, error) {
	statusPath := svc.Meta["statusPath"]
	if statusPath == "" {
		statusPath = "/nginx_status"
	}

	body, err := HTTPGet(ctx, svc.BaseURL+statusPath)
	if err != nil {
		return nil, fmt.Errorf("could not reach Nginx stub_status at %s%s: %w", svc.BaseURL, statusPath, err)
	}

	parsed, err := parseStubStatus(string(body))
	if err != nil {
		return nil, fmt.Errorf("invalid stub_status response: %w", err)
	}

	dropped := parsed.accepts - parsed.handled

	return &ServiceStats{
		PluginID: p.ID(),
		Name:     p.Name(),
		Icon:     p.Icon(),
		Status:   "running",
		Summary: []StatItem{
			{Label: "Active Connections", Value: FormatNumber(parsed.activeConnections), Type: "number"},
			{Label: "Total Requests", Value: FormatNumber(parsed.requests), Type: "number"},
			{Label: "Reading", Value: FormatNumber(parsed.reading), Type: "number"},
			{Label: "Writing", Value: FormatNumber(parsed.writing), Type: "number"},
		},
		Stats: map[string]interface{}{
			"activeConnections": parsed.activeConnections,
			"accepts":           parsed.accepts,
			"handled":           parsed.handled,
			"requests":          parsed.requests,
			"reading":           parsed.reading,
			"writing":           parsed.writing,
			"waiting":           parsed.waiting,
			"dropped":           dropped,
		},
	}, nil
}

// stubStatus holds parsed Nginx stub_status values.
type stubStatus struct {
	activeConnections int64
	accepts           int64
	handled           int64
	requests          int64
	reading           int64
	writing           int64
	waiting           int64
}

var activeRe = regexp.MustCompile(`Active connections:\s*(\d+)`)
var rwwRe = regexp.MustCompile(`Reading:\s*(\d+)\s+Writing:\s*(\d+)\s+Waiting:\s*(\d+)`)

// parseStubStatus parses Nginx stub_status output:
//
//	Active connections: 291
//	server accepts handled requests
//	 16630948 16630946 31070465
//	Reading: 6 Writing: 179 Waiting: 106
func parseStubStatus(body string) (*stubStatus, error) {
	s := &stubStatus{}

	if m := activeRe.FindStringSubmatch(body); len(m) > 1 {
		s.activeConnections, _ = strconv.ParseInt(m[1], 10, 64)
	} else {
		return nil, fmt.Errorf("missing Active connections line")
	}

	// Find the line with three numbers: accepts handled requests
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) == 3 {
			a, e1 := strconv.ParseInt(fields[0], 10, 64)
			h, e2 := strconv.ParseInt(fields[1], 10, 64)
			r, e3 := strconv.ParseInt(fields[2], 10, 64)
			if e1 == nil && e2 == nil && e3 == nil && a > 0 {
				s.accepts = a
				s.handled = h
				s.requests = r
			}
		}
	}

	if m := rwwRe.FindStringSubmatch(body); len(m) > 3 {
		s.reading, _ = strconv.ParseInt(m[1], 10, 64)
		s.writing, _ = strconv.ParseInt(m[2], 10, 64)
		s.waiting, _ = strconv.ParseInt(m[3], 10, 64)
	}

	return s, nil
}
