package collector

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type ContainerStats struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Image           string  `json:"image"`
	Status          string  `json:"status"`
	CPUPercent      float64 `json:"cpuPercent"`
	MemoryUsageMB   float64 `json:"memoryUsageMB"`
	MemoryLimitMB   float64 `json:"memoryLimitMB"`
	NetworkRxBytes  uint64  `json:"networkRxBytes"`
	NetworkTxBytes  uint64  `json:"networkTxBytes"`
	BlockReadBytes  uint64  `json:"blockReadBytes"`
	BlockWriteBytes uint64  `json:"blockWriteBytes"`
	PIDs            uint64  `json:"pids"`
	StartedAt       string  `json:"startedAt"`
}

type DockerCollector struct {
	socketPath string
}

func NewDockerCollector(socketPath string) *DockerCollector {
	return &DockerCollector{
		socketPath: socketPath,
	}
}

func (dc *DockerCollector) Collect() []ContainerStats {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+dc.socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return []ContainerStats{}
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return []ContainerStats{}
	}

	results := make([]ContainerStats, 0, len(containers))

	for _, c := range containers {
		cs := ContainerStats{
			ID:     c.ID[:12],
			Name:   cleanContainerName(c.Names),
			Image:  c.Image,
			Status: normalizeStatus(c.State),
		}

		// Only fetch resource stats for running containers
		if c.State == "running" {
			dc.fillRunningStats(ctx, cli, c.ID, &cs)
		}

		// Get started time from container inspect
		info, err := cli.ContainerInspect(ctx, c.ID)
		if err == nil && info.State != nil {
			cs.StartedAt = info.State.StartedAt
		}

		results = append(results, cs)
	}

	return results
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
