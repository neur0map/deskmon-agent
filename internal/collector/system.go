package collector

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type SystemStats struct {
	CPU     CPUStats     `json:"cpu"`
	Memory  MemoryStats  `json:"memory"`
	Disk    DiskStats    `json:"disk"`
	Network NetworkStats `json:"network"`
	Uptime  int64        `json:"uptimeSeconds"`
}

type CPUStats struct {
	UsagePercent float64 `json:"usagePercent"`
	CoreCount    int     `json:"coreCount"`
	Temperature  float64 `json:"temperature"`
}

type MemoryStats struct {
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

type DiskStats struct {
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

type NetworkStats struct {
	DownloadBytesPerSec float64 `json:"downloadBytesPerSec"`
	UploadBytesPerSec   float64 `json:"uploadBytesPerSec"`
}

type cpuSample struct {
	total uint64
	idle  uint64
}

type netSample struct {
	rxBytes   uint64
	txBytes   uint64
	timestamp time.Time
}

type SystemCollector struct {
	mu          sync.RWMutex
	cpuUsage    float64
	coreCount   int
	prevCPU     cpuSample
	prevNet     netSample
	netDownload float64
	netUpload   float64
	stopCh      chan struct{}
}

func NewSystemCollector() *SystemCollector {
	sc := &SystemCollector{
		stopCh: make(chan struct{}),
	}
	sc.coreCount = countCPUCores()
	// Take initial samples so first delta is meaningful
	sc.prevCPU = readCPUSample()
	sc.prevNet = readNetSample()
	return sc
}

func (sc *SystemCollector) Start() {
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				sc.sample()
			case <-sc.stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

func (sc *SystemCollector) Stop() {
	close(sc.stopCh)
}

func (sc *SystemCollector) sample() {
	// CPU delta
	cur := readCPUSample()
	sc.mu.Lock()
	totalDelta := cur.total - sc.prevCPU.total
	idleDelta := cur.idle - sc.prevCPU.idle
	if totalDelta > 0 {
		sc.cpuUsage = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
		sc.cpuUsage = math.Round(sc.cpuUsage*100) / 100
	}
	sc.prevCPU = cur

	// Network delta
	netCur := readNetSample()
	elapsed := netCur.timestamp.Sub(sc.prevNet.timestamp).Seconds()
	if elapsed > 0 {
		sc.netDownload = float64(netCur.rxBytes-sc.prevNet.rxBytes) / elapsed
		sc.netUpload = float64(netCur.txBytes-sc.prevNet.txBytes) / elapsed
		sc.netDownload = math.Round(sc.netDownload*100) / 100
		sc.netUpload = math.Round(sc.netUpload*100) / 100
	}
	sc.prevNet = netCur
	sc.mu.Unlock()
}

func (sc *SystemCollector) Collect() SystemStats {
	sc.mu.RLock()
	cpuUsage := sc.cpuUsage
	download := sc.netDownload
	upload := sc.netUpload
	sc.mu.RUnlock()

	mem := readMemory()
	disk := readDisk()
	temp := readTemperature()
	uptime := readUptime()

	return SystemStats{
		CPU: CPUStats{
			UsagePercent: cpuUsage,
			CoreCount:    sc.coreCount,
			Temperature:  temp,
		},
		Memory:  mem,
		Disk:    disk,
		Network: NetworkStats{
			DownloadBytesPerSec: download,
			UploadBytesPerSec:   upload,
		},
		Uptime: uptime,
	}
}

func countCPUCores() int {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 1
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "processor") {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func readCPUSample() cpuSample {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return cpuSample{}
			}
			var total, idle uint64
			for i := 1; i < len(fields); i++ {
				val, _ := strconv.ParseUint(fields[i], 10, 64)
				total += val
				if i == 4 { // idle is the 4th value (index 4 in fields)
					idle = val
				}
			}
			return cpuSample{total: total, idle: idle}
		}
	}
	return cpuSample{}
}

func readNetSample() netSample {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return netSample{timestamp: time.Now()}
	}
	defer f.Close()

	var totalRx, totalTx uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip header lines
		if strings.Contains(line, "|") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		// Skip loopback
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		totalRx += rx
		totalTx += tx
	}

	return netSample{
		rxBytes:   totalRx,
		txBytes:   totalTx,
		timestamp: time.Now(),
	}
}

func readMemory() MemoryStats {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemoryStats{}
	}
	defer f.Close()

	var total, available uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			total = parseMemInfoValue(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			available = parseMemInfoValue(line)
		}
	}

	// /proc/meminfo reports in kB
	totalBytes := total * 1024
	availableBytes := available * 1024
	usedBytes := totalBytes - availableBytes

	return MemoryStats{
		UsedBytes:  usedBytes,
		TotalBytes: totalBytes,
	}
}

func parseMemInfoValue(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	val, _ := strconv.ParseUint(fields[1], 10, 64)
	return val
}

func readDisk() DiskStats {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return DiskStats{}
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	usedBytes := totalBytes - freeBytes

	return DiskStats{
		UsedBytes:  usedBytes,
		TotalBytes: totalBytes,
	}
}

func readTemperature() float64 {
	matches, err := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	if err != nil || len(matches) == 0 {
		return 0
	}

	var maxTemp float64
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		// Kernel reports millidegrees
		temp := val / 1000.0
		if temp > maxTemp {
			maxTemp = temp
		}
	}

	return math.Round(maxTemp*10) / 10
}

func readUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return int64(val)
}

// FormatBytes is a helper for logging/debugging, not used in API responses
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
