package services

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/neur0map/deskmon-agent/internal/collector"
)

const (
	detectInterval  = 30 * time.Second
	collectInterval = 10 * time.Second
	collectTimeout  = 8 * time.Second
)

// ServiceDetector runs service detection and stats collection in the background.
type ServiceDetector struct {
	mu             sync.RWMutex
	detected       map[string]*DetectedService
	cachedStats    []ServiceStats
	serviceConfigs map[string]map[string]string // pluginID → key → value
	dockerSocket   string
	stopCh         chan struct{}

	// SSE broadcast
	Broadcast *collector.Broadcaster[[]ServiceStats]
}

// NewServiceDetector creates a detector that will use the given Docker socket
// for container-based detection.
func NewServiceDetector(dockerSocket string) *ServiceDetector {
	return &ServiceDetector{
		detected:       make(map[string]*DetectedService),
		serviceConfigs: make(map[string]map[string]string),
		dockerSocket:   dockerSocket,
		stopCh:         make(chan struct{}),
		Broadcast:      collector.NewBroadcaster[[]ServiceStats](),
	}
}

// SetServiceConfig stores a configuration key/value for a plugin.
// The value is injected into the DetectedService.Meta during detection.
func (sd *ServiceDetector) SetServiceConfig(pluginID, key, value string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if sd.serviceConfigs[pluginID] == nil {
		sd.serviceConfigs[pluginID] = make(map[string]string)
	}
	sd.serviceConfigs[pluginID][key] = value

	// Also inject into already-detected service so the next collection picks it up
	if svc, ok := sd.detected[pluginID]; ok {
		if svc.Meta == nil {
			svc.Meta = make(map[string]string)
		}
		svc.Meta[key] = value
	}

	log.Printf("services: config set for %s: %s=<redacted>", pluginID, key)
}

// Start begins background detection and collection loops.
// Detection runs asynchronously so the HTTP server can start immediately.
func (sd *ServiceDetector) Start() {
	go func() {
		// Run initial detection + collection in the background
		sd.runDetection()
		sd.runCollection()

		detectTicker := time.NewTicker(detectInterval)
		collectTicker := time.NewTicker(collectInterval)
		defer detectTicker.Stop()
		defer collectTicker.Stop()

		for {
			select {
			case <-detectTicker.C:
				sd.runDetection()
				sd.runCollection()
			case <-collectTicker.C:
				sd.runCollection()
			case <-sd.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the background loops.
func (sd *ServiceDetector) Stop() {
	close(sd.stopCh)
}

// DebugInfo returns a snapshot of the detection environment for diagnostics.
type DebugSnapshot struct {
	Plugins      []string            `json:"plugins"`
	Containers   []ContainerInfo     `json:"containers"`
	Processes    []string            `json:"processes"`
	ProcessPorts map[string][]int    `json:"processPorts"`
	Detected     map[string]string   `json:"detected"` // pluginID → baseURL
	Stats        []ServiceStats      `json:"stats"`
}

func (sd *ServiceDetector) DebugInfo() DebugSnapshot {
	env := BuildDetectionEnv(sd.dockerSocket)

	plugins := RegisteredPlugins()
	pluginNames := make([]string, len(plugins))
	for i, p := range plugins {
		pluginNames[i] = p.ID()
	}

	procs := make([]string, 0, len(env.Processes))
	for name := range env.Processes {
		procs = append(procs, name)
	}
	sort.Strings(procs)

	sd.mu.RLock()
	detected := make(map[string]string, len(sd.detected))
	for id, svc := range sd.detected {
		detected[id] = svc.BaseURL
	}
	stats := make([]ServiceStats, len(sd.cachedStats))
	copy(stats, sd.cachedStats)
	sd.mu.RUnlock()

	return DebugSnapshot{
		Plugins:      pluginNames,
		Containers:   env.Containers,
		Processes:    procs,
		ProcessPorts: env.ProcessPorts,
		Detected:     detected,
		Stats:        stats,
	}
}

// Collect returns the latest cached service stats.
func (sd *ServiceDetector) Collect() []ServiceStats {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	if len(sd.cachedStats) == 0 {
		return []ServiceStats{}
	}
	result := make([]ServiceStats, len(sd.cachedStats))
	copy(result, sd.cachedStats)
	return result
}

// runDetection scans for services using all registered plugins.
// Network probes run without holding the lock so SetServiceConfig is not blocked.
func (sd *ServiceDetector) runDetection() {
	plugins := RegisteredPlugins()
	if len(plugins) == 0 {
		log.Println("services: no plugins registered")
		return
	}

	log.Printf("services: running detection with %d plugins", len(plugins))
	env := BuildDetectionEnv(sd.dockerSocket)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Run detection without holding the lock (network I/O happens here)
	newDetected := make(map[string]*DetectedService)
	for _, p := range plugins {
		svc := p.Detect(ctx, env)
		if svc != nil {
			newDetected[p.ID()] = svc
		} else {
			log.Printf("services: plugin %s did not detect a service", p.ID())
		}
	}

	// Brief lock to merge results
	sd.mu.Lock()
	defer sd.mu.Unlock()

	seen := make(map[string]bool, len(newDetected))
	for id, svc := range newDetected {
		seen[id] = true
		if _, exists := sd.detected[id]; !exists {
			log.Printf("services: detected %s at %s", svc.Name, svc.BaseURL)
		}
		// Inject stored service configs into Meta
		if cfg, ok := sd.serviceConfigs[id]; ok {
			for k, v := range cfg {
				svc.Meta[k] = v
			}
		}
		// Preserve version from previous detection if new one is empty
		if prev, exists := sd.detected[id]; exists && svc.Version == "" {
			svc.Version = prev.Version
		}
		sd.detected[id] = svc
	}

	// Remove services that are no longer detected
	for id, svc := range sd.detected {
		if !seen[id] {
			log.Printf("services: %s no longer detected", svc.Name)
			delete(sd.detected, id)
		}
	}
}

// runCollection fetches stats from all detected services.
func (sd *ServiceDetector) runCollection() {
	sd.mu.RLock()
	detected := make(map[string]*DetectedService, len(sd.detected))
	for k, v := range sd.detected {
		detected[k] = v
	}
	sd.mu.RUnlock()

	if len(detected) == 0 {
		sd.mu.Lock()
		sd.cachedStats = []ServiceStats{}
		sd.mu.Unlock()
		return
	}

	plugins := RegisteredPlugins()
	pluginMap := make(map[string]ServicePlugin, len(plugins))
	for _, p := range plugins {
		pluginMap[p.ID()] = p
	}

	// Collect from all detected services concurrently
	type result struct {
		stats ServiceStats
	}
	results := make(chan result, len(detected))

	var wg sync.WaitGroup
	for id, svc := range detected {
		p, ok := pluginMap[id]
		if !ok {
			continue
		}

		wg.Add(1)
		go func(plugin ServicePlugin, service *DetectedService) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
			defer cancel()

			stats, err := plugin.Collect(ctx, service)
			if err != nil {
				results <- result{stats: ServiceStats{
					PluginID: plugin.ID(),
					Name:     plugin.Name(),
					Icon:     plugin.Icon(),
					Status:   "error",
					Summary:  []StatItem{},
					Stats:    map[string]interface{}{},
					Error:    err.Error(),
					URL:      service.BaseURL,
				}}
				return
			}

			stats.URL = service.BaseURL
			results <- result{stats: *stats}
		}(p, svc)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var stats []ServiceStats
	for r := range results {
		stats = append(stats, r.stats)
	}

	if stats == nil {
		stats = []ServiceStats{}
	}

	sd.mu.Lock()
	sd.cachedStats = stats
	sd.mu.Unlock()

	// Broadcast to SSE subscribers
	broadcast := make([]ServiceStats, len(stats))
	copy(broadcast, stats)
	sd.Broadcast.Send(broadcast)
}
