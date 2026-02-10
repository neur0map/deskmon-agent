package collector

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type PortMapping struct {
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

type ContainerStats struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Image           string        `json:"image"`
	Status          string        `json:"status"`
	CPUPercent      float64       `json:"cpuPercent"`
	MemoryUsageMB   float64       `json:"memoryUsageMB"`
	MemoryLimitMB   float64       `json:"memoryLimitMB"`
	NetworkRxBytes  uint64        `json:"networkRxBytes"`
	NetworkTxBytes  uint64        `json:"networkTxBytes"`
	BlockReadBytes  uint64        `json:"blockReadBytes"`
	BlockWriteBytes uint64        `json:"blockWriteBytes"`
	PIDs            uint64        `json:"pids"`
	StartedAt       string        `json:"startedAt"`
	Ports           []PortMapping `json:"ports"`
	RestartCount    int           `json:"restartCount"`
	HealthStatus    string        `json:"healthStatus"`
}

type DockerCollector struct {
	socketPath string
	mu         sync.RWMutex
	cached     []ContainerStats
	stopCh     chan struct{}

	// SSE broadcast
	Broadcast *Broadcaster[[]ContainerStats]
}

func NewDockerCollector(socketPath string) *DockerCollector {
	return &DockerCollector{
		socketPath: socketPath,
		cached:     []ContainerStats{},
		stopCh:     make(chan struct{}),
		Broadcast:  NewBroadcaster[[]ContainerStats](),
	}
}

// Start begins background collection on a 5-second ticker.
func (dc *DockerCollector) Start() {
	// Run initial collection immediately
	dc.refresh()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				dc.refresh()
			case <-dc.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the background collection loop.
func (dc *DockerCollector) Stop() {
	close(dc.stopCh)
}

// Collect returns the latest cached container stats (non-blocking).
func (dc *DockerCollector) Collect() []ContainerStats {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	result := make([]ContainerStats, len(dc.cached))
	copy(result, dc.cached)
	return result
}

// refresh fetches live Docker stats and updates the cache.
func (dc *DockerCollector) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+dc.socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return
	}

	results := make([]ContainerStats, len(containers))

	var wg sync.WaitGroup
	for i, c := range containers {
		results[i] = ContainerStats{
			ID:           c.ID[:12],
			Name:         cleanContainerName(c.Names),
			Image:        c.Image,
			Status:       normalizeStatus(c.State),
			Ports:        []PortMapping{},
			HealthStatus: "none",
		}

		wg.Add(1)
		go func(idx int, ctr container.Summary) {
			defer wg.Done()

			perCtx, perCancel := context.WithTimeout(ctx, 6*time.Second)
			defer perCancel()

			if ctr.State == "running" {
				dc.fillRunningStats(perCtx, cli, ctr.ID, &results[idx])
			}

			info, inspectErr := cli.ContainerInspect(perCtx, ctr.ID)
			if inspectErr == nil {
				if info.State != nil {
					results[idx].StartedAt = info.State.StartedAt
					if info.State.Health != nil {
						results[idx].HealthStatus = info.State.Health.Status
					}
				}
				results[idx].RestartCount = info.RestartCount
				if info.NetworkSettings != nil {
					results[idx].Ports = extractPorts(info.NetworkSettings.Ports)
				}
			}
		}(i, c)
	}
	wg.Wait()

	dc.mu.Lock()
	dc.cached = results
	dc.mu.Unlock()

	// Broadcast to SSE subscribers
	broadcast := make([]ContainerStats, len(results))
	copy(broadcast, results)
	dc.Broadcast.Send(broadcast)
}

func (dc *DockerCollector) fillRunningStats(ctx context.Context, cli *client.Client, containerID string, cs *ContainerStats) {
	statsResp, err := cli.ContainerStats(ctx, containerID, false)
	if err != nil {
		return
	}
	defer statsResp.Body.Close()

	var stats container.StatsResponse
	data, err := io.ReadAll(statsResp.Body)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &stats); err != nil {
		return
	}

	// CPU percent using PreCPUStats (previous sample provided by Docker)
	cs.CPUPercent = calculateCPUPercent(&stats)

	// Memory
	cs.MemoryUsageMB = math.Round(float64(stats.MemoryStats.Usage)/1024/1024*100) / 100
	if stats.MemoryStats.Limit > 0 && stats.MemoryStats.Limit < 1<<62 {
		cs.MemoryLimitMB = math.Round(float64(stats.MemoryStats.Limit)/1024/1024*100) / 100
	}

	// Network - sum across all interfaces
	for _, netStats := range stats.Networks {
		cs.NetworkRxBytes += netStats.RxBytes
		cs.NetworkTxBytes += netStats.TxBytes
	}

	// Block I/O
	for _, bioEntry := range stats.BlkioStats.IoServiceBytesRecursive {
		switch bioEntry.Op {
		case "read", "Read":
			cs.BlockReadBytes += bioEntry.Value
		case "write", "Write":
			cs.BlockWriteBytes += bioEntry.Value
		}
	}

	// PIDs
	cs.PIDs = stats.PidsStats.Current
}

func calculateCPUPercent(stats *container.StatsResponse) float64 {
	curContainer := stats.CPUStats.CPUUsage.TotalUsage
	prevContainer := stats.PreCPUStats.CPUUsage.TotalUsage
	curSystem := stats.CPUStats.SystemUsage
	prevSystem := stats.PreCPUStats.SystemUsage

	numCores := len(stats.CPUStats.CPUUsage.PercpuUsage)
	if numCores == 0 {
		numCores = int(stats.CPUStats.OnlineCPUs)
	}
	if numCores == 0 {
		numCores = 1
	}

	containerDelta := float64(curContainer - prevContainer)
	systemDelta := float64(curSystem - prevSystem)

	if systemDelta <= 0 || containerDelta <= 0 {
		return 0
	}

	cpuPercent := (containerDelta / systemDelta) * float64(numCores) * 100.0
	return math.Round(cpuPercent*100) / 100
}

func cleanContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	// Docker prepends "/" to container names
	return strings.TrimPrefix(names[0], "/")
}

func normalizeStatus(state string) string {
	switch state {
	case "running":
		return "running"
	case "restarting":
		return "restarting"
	default:
		return "stopped"
	}
}

func extractPorts(portMap nat.PortMap) []PortMapping {
	var ports []PortMapping
	for containerPort, bindings := range portMap {
		cPort := containerPort.Int()
		proto := containerPort.Proto()
		for _, binding := range bindings {
			hPort, err := strconv.Atoi(binding.HostPort)
			if err != nil {
				continue
			}
			ports = append(ports, PortMapping{
				HostPort:      hPort,
				ContainerPort: cPort,
				Protocol:      proto,
			})
		}
	}
	if ports == nil {
		return []PortMapping{}
	}
	return ports
}
