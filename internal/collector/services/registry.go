package services

import "context"

// ServicePlugin is the interface every service integration must implement.
// Contributors add a single file with an init() that calls Register().
type ServicePlugin interface {
	// ID returns a unique lowercase identifier, e.g. "pihole".
	ID() string
	// Name returns the human-readable service name, e.g. "Pi-hole".
	Name() string
	// Icon returns an SF Symbol name for the macOS app.
	Icon() string
	// Detect checks whether this service is running and returns details, or nil.
	Detect(ctx context.Context, env *DetectionEnv) *DetectedService
	// Collect fetches current stats from a previously detected service.
	Collect(ctx context.Context, svc *DetectedService) (*ServiceStats, error)
}

// DetectedService holds information about a discovered service instance.
type DetectedService struct {
	PluginID string
	Name     string
	Icon     string
	BaseURL  string
	Version  string            // e.g. "v5", "v6"
	Meta     map[string]string // plugin-specific metadata
}

// ServiceStats is the JSON payload returned to the macOS app for each service.
type ServiceStats struct {
	PluginID string                 `json:"pluginId"`
	Name     string                 `json:"name"`
	Icon     string                 `json:"icon"`
	Status   string                 `json:"status"` // "running", "stopped", "error"
	Summary  []StatItem             `json:"summary"`
	Stats    map[string]interface{} `json:"stats"`
	Error    string                 `json:"error,omitempty"`
}

// StatItem is a single key-value metric shown on the service card.
type StatItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Type  string `json:"type"` // "number", "percent", "status", "text"
}

// ContainerInfo is a lightweight container record used during detection.
type ContainerInfo struct {
	Name      string
	Image     string
	State     string // "running", "stopped", etc.
	HostPorts []int  // host-side ports
}

var registry []ServicePlugin

// Register adds a plugin to the global registry. Call from init().
func Register(p ServicePlugin) {
	registry = append(registry, p)
}

// RegisteredPlugins returns all registered service plugins.
func RegisteredPlugins() []ServicePlugin {
	return registry
}
