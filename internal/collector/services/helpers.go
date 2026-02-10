package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// DetectionEnv provides helpers that plugins use to discover services.
type DetectionEnv struct {
	Containers []ContainerInfo
	Processes  map[string]bool // process name → exists
}

// FindDockerImage returns the first container whose image contains the match string.
func (e *DetectionEnv) FindDockerImage(match string) *ContainerInfo {
	lower := strings.ToLower(match)
	for i := range e.Containers {
		if strings.Contains(strings.ToLower(e.Containers[i].Image), lower) {
			return &e.Containers[i]
		}
	}
	return nil
}

// HasProcess returns true if a process with the given name is running.
func (e *DetectionEnv) HasProcess(name string) bool {
	return e.Processes[name]
}

// ProbeHTTP tries an HTTP GET on localhost at each port+path combination.
// Returns the base URL (http://host:port) of the first successful probe, or "".
func (e *DetectionEnv) ProbeHTTP(ports []int, path string) string {
	cl := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects, just check status
		},
	}
	hosts := []string{"127.0.0.1", "localhost"}
	for _, port := range ports {
		for _, host := range hosts {
			url := fmt.Sprintf("http://%s:%d%s", host, port, path)
			resp, err := cl.Get(url)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				base := fmt.Sprintf("http://%s:%d", host, port)
				log.Printf("services: probe hit %s (HTTP %d)", url, resp.StatusCode)
				return base
			}
		}
	}
	return ""
}

// HTTPGet performs a GET request with context and returns the response body.
func HTTPGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
}

// BuildDetectionEnv constructs a DetectionEnv by querying Docker and /proc.
func BuildDetectionEnv(dockerSocket string) *DetectionEnv {
	containers := listDockerContainers(dockerSocket)
	processes := scanProcessNames()
	log.Printf("services: detection env — %d containers, %d processes", len(containers), len(processes))
	if len(containers) > 0 {
		for _, c := range containers {
			log.Printf("services:   container: %s image=%s state=%s ports=%v", c.Name, c.Image, c.State, c.HostPorts)
		}
	}
	return &DetectionEnv{
		Containers: containers,
		Processes:  processes,
	}
}

// listDockerContainers does a lightweight container list (no stats).
func listDockerContainers(socketPath string) []ContainerInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Printf("services: docker client init error: %v", err)
		return nil
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		log.Printf("services: docker list error: %v", err)
		return nil
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, c := range containers {
		var hostPorts []int
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				hostPorts = append(hostPorts, int(p.PublicPort))
			}
		}

		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		result = append(result, ContainerInfo{
			Name:      name,
			Image:     c.Image,
			State:     c.State,
			HostPorts: hostPorts,
		})
	}
	return result
}

// scanProcessNames reads /proc/*/comm to build a set of running process names.
func scanProcessNames() map[string]bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		log.Printf("services: cannot read /proc: %v", err)
		return make(map[string]bool)
	}

	names := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue
		}
		names[strings.TrimSpace(string(data))] = true
	}
	return names
}

// FormatNumber formats an integer with comma separators (e.g. 12345 → "12,345").
func FormatNumber(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}
