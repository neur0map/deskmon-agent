package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

func init() {
	Register(&TraefikPlugin{})
}

// TraefikPlugin detects and collects stats from Traefik's built-in API.
type TraefikPlugin struct{}

func (p *TraefikPlugin) ID() string   { return "traefik" }
func (p *TraefikPlugin) Name() string { return "Traefik" }
func (p *TraefikPlugin) Icon() string { return "arrow.triangle.branch" }

func (p *TraefikPlugin) Detect(ctx context.Context, env *DetectionEnv) *DetectedService {
	base := &DetectedService{
		PluginID: p.ID(),
		Name:     p.Name(),
		Icon:     p.Icon(),
		Meta:     make(map[string]string),
	}

	// Strategy 1: Docker container with "traefik" in image name
	if c := env.FindDockerImage("traefik"); c != nil && c.State == "running" {
		ports := append(c.HostPorts, 8080, 8443)
		if url := env.ProbeHTTP(ports, "/api/overview"); url != "" {
			base.BaseURL = url
			log.Printf("services: traefik detected via docker (%s) at %s", c.Image, url)
			return base
		}
	}

	// Strategy 2: traefik process running
	if env.HasProcess("traefik") {
		if url := env.ProbeHTTP([]int{8080, 8443, 9090}, "/api/overview"); url != "" {
			base.BaseURL = url
			log.Printf("services: traefik detected via process at %s", url)
			return base
		}
	}

	// Strategy 3: Blind probe on common Traefik dashboard ports
	if url := env.ProbeHTTP([]int{8080, 8443}, "/api/overview"); url != "" {
		base.BaseURL = url
		log.Printf("services: traefik detected via port probe at %s", url)
		return base
	}

	return nil
}

func (p *TraefikPlugin) Collect(ctx context.Context, svc *DetectedService) (*ServiceStats, error) {
	stats := &ServiceStats{
		PluginID: p.ID(),
		Name:     p.Name(),
		Icon:     p.Icon(),
		Status:   "running",
		Stats:    make(map[string]interface{}),
	}

	// Fetch /api/overview
	overviewData, err := HTTPGet(ctx, svc.BaseURL+"/api/overview")
	if err != nil {
		return nil, fmt.Errorf("could not reach Traefik API at %s: %w", svc.BaseURL, err)
	}

	var overview traefikOverview
	if err := json.Unmarshal(overviewData, &overview); err != nil {
		return nil, fmt.Errorf("invalid Traefik response: %w", err)
	}

	httpRouters := overview.HTTP.Routers.Total
	httpServices := overview.HTTP.Services.Total
	httpMiddlewares := overview.HTTP.Middlewares.Total
	tcpRouters := overview.TCP.Routers.Total
	tcpServices := overview.TCP.Services.Total
	udpRouters := overview.UDP.Routers.Total
	udpServices := overview.UDP.Services.Total

	totalRouters := httpRouters + tcpRouters + udpRouters
	totalServices := httpServices + tcpServices + udpServices

	// Fetch /api/entrypoints
	var entrypointCount int64
	if epData, err := HTTPGet(ctx, svc.BaseURL+"/api/entrypoints"); err == nil {
		var entrypoints []interface{}
		if json.Unmarshal(epData, &entrypoints) == nil {
			entrypointCount = int64(len(entrypoints))
		}
	}

	// Count warnings (routers/services with errors)
	httpRouterWarnings := overview.HTTP.Routers.Errors
	httpServiceWarnings := overview.HTTP.Services.Errors
	totalWarnings := httpRouterWarnings + httpServiceWarnings

	stats.Summary = []StatItem{
		{Label: "Routers", Value: FormatNumber(int64(totalRouters)), Type: "number"},
		{Label: "Services", Value: FormatNumber(int64(totalServices)), Type: "number"},
		{Label: "Entrypoints", Value: FormatNumber(entrypointCount), Type: "number"},
		{Label: "Middlewares", Value: FormatNumber(int64(httpMiddlewares)), Type: "number"},
	}

	stats.Stats = map[string]interface{}{
		"httpRouters":     httpRouters,
		"httpServices":    httpServices,
		"httpMiddlewares": httpMiddlewares,
		"tcpRouters":      tcpRouters,
		"tcpServices":     tcpServices,
		"udpRouters":      udpRouters,
		"udpServices":     udpServices,
		"entrypoints":     entrypointCount,
		"totalRouters":    totalRouters,
		"totalServices":   totalServices,
		"warnings":        totalWarnings,
	}

	if totalWarnings > 0 {
		stats.Status = "degraded"
	}

	return stats, nil
}

// Traefik /api/overview response structure
type traefikOverview struct {
	HTTP traefikProto `json:"http"`
	TCP  traefikProto `json:"tcp"`
	UDP  traefikProto `json:"udp"`
}

type traefikProto struct {
	Routers     traefikCount `json:"routers"`
	Services    traefikCount `json:"services"`
	Middlewares traefikCount `json:"middlewares"`
}

type traefikCount struct {
	Total    int `json:"total"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}
