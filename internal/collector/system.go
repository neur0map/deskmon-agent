package collector

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type SystemStats struct {
	CPU     CPUStats      `json:"cpu"`
	Memory  MemoryStats   `json:"memory"`
	Disks   []DiskInfo    `json:"disks"`
	Network NetworkReport `json:"network"`
	Uptime  int64         `json:"uptimeSeconds"`
}

type CPUStats struct {
	UsagePercent         float64 `json:"usagePercent"`
	CoreCount            int     `json:"coreCount"`
	Temperature          float64 `json:"temperature"`
	TemperatureAvailable bool    `json:"temperatureAvailable"`
}

type MemoryStats struct {
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

type DiskInfo struct {
	MountPoint string `json:"mountPoint"`
	Device     string `json:"device"`
	FsType     string `json:"fsType"`
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

type InterfaceStats struct {
	DownloadBytesPerSec float64 `json:"downloadBytesPerSec"`
	UploadBytesPerSec   float64 `json:"uploadBytesPerSec"`
	RxErrors            uint64  `json:"rxErrors,omitempty"`
	RxDrops             uint64  `json:"rxDrops,omitempty"`
	TxErrors            uint64  `json:"txErrors,omitempty"`
	TxDrops             uint64  `json:"txDrops,omitempty"`
}

type NetworkReport struct {
	Physical InterfaceStats  `json:"physical"`
	Virtual  *InterfaceStats `json:"virtual,omitempty"`
}

type ProcessInfo struct {
	PID           int32   `json:"pid"`
	Name          string  `json:"name"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryMB      float64 `json:"memoryMB"`
	MemoryPercent float64 `json:"memoryPercent"`
	Command       string  `json:"command,omitempty"`
	User          string  `json:"user,omitempty"`
}

type processCPUSample struct {
	utime     uint64
	stime     uint64
	timestamp time.Time
}

type cpuSample struct {
	total uint64
	idle  uint64
}

type netSample struct {
	physRx      uint64
	physTx      uint64
	physRxErr   uint64
	physRxDrop  uint64
	physTxErr   uint64
	physTxDrop  uint64
	virtRx      uint64
	virtTx      uint64
	virtRxErr   uint64
	virtRxDrop  uint64
	virtTxErr   uint64
	virtTxDrop  uint64
	timestamp   time.Time
}

// SystemEvent is the payload broadcast each time system stats are sampled.
type SystemEvent struct {
	System    SystemStats   `json:"system"`
	Processes []ProcessInfo `json:"processes"`
}

type SystemCollector struct {
	mu          sync.RWMutex
	cpuUsage    float64
	coreCount   int
	prevCPU     cpuSample
	prevNet     netSample
	netPhysical InterfaceStats
	netVirtual  InterfaceStats
	stopCh      chan struct{}

	// Process monitoring
	prevProcCPU  map[int32]processCPUSample
	smoothedCPU  map[int32]float64 // EMA-smoothed CPU per process
	topProcesses []ProcessInfo
	totalMemKB   uint64

	// SSE broadcast
	Broadcast *Broadcaster[SystemEvent]
}

func NewSystemCollector() *SystemCollector {
	sc := &SystemCollector{
		stopCh:      make(chan struct{}),
		prevProcCPU: make(map[int32]processCPUSample),
		smoothedCPU: make(map[int32]float64),
		Broadcast:   NewBroadcaster[SystemEvent](),
	}
	sc.coreCount = countCPUCores()
	sc.totalMemKB = readTotalMemKB()
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
		sc.netPhysical = calcInterfaceStats(
			sc.prevNet.physRx, netCur.physRx,
			sc.prevNet.physTx, netCur.physTx,
			sc.prevNet.physRxErr, netCur.physRxErr,
			sc.prevNet.physRxDrop, netCur.physRxDrop,
			sc.prevNet.physTxErr, netCur.physTxErr,
			sc.prevNet.physTxDrop, netCur.physTxDrop,
			elapsed,
		)
		sc.netVirtual = calcInterfaceStats(
			sc.prevNet.virtRx, netCur.virtRx,
			sc.prevNet.virtTx, netCur.virtTx,
			sc.prevNet.virtRxErr, netCur.virtRxErr,
			sc.prevNet.virtRxDrop, netCur.virtRxDrop,
			sc.prevNet.virtTxErr, netCur.virtTxErr,
			sc.prevNet.virtTxDrop, netCur.virtTxDrop,
			elapsed,
		)
	}
	sc.prevNet = netCur

	// Process sampling
	sc.sampleProcesses()

	// Snapshot for broadcast while holding the lock
	cpuUsage := sc.cpuUsage
	phys := sc.netPhysical
	virt := sc.netVirtual
	procs := make([]ProcessInfo, len(sc.topProcesses))
	copy(procs, sc.topProcesses)

	sc.mu.Unlock()

	// Broadcast full system snapshot (non-blocking)
	mem := readMemory()
	disks := readDisks()
	temp, tempAvail := readTemperature()
	uptime := readUptime()

	netReport := NetworkReport{Physical: phys}
	if virt.DownloadBytesPerSec > 0 || virt.UploadBytesPerSec > 0 ||
		virt.RxErrors > 0 || virt.TxErrors > 0 || virt.RxDrops > 0 || virt.TxDrops > 0 {
		netReport.Virtual = &virt
	}

	sc.Broadcast.Send(SystemEvent{
		System: SystemStats{
			CPU: CPUStats{
				UsagePercent:         cpuUsage,
				CoreCount:            sc.coreCount,
				Temperature:          temp,
				TemperatureAvailable: tempAvail,
			},
			Memory:  mem,
			Disks:   disks,
			Network: netReport,
			Uptime:  uptime,
		},
		Processes: procs,
	})
}

func (sc *SystemCollector) Collect() SystemStats {
	sc.mu.RLock()
	cpuUsage := sc.cpuUsage
	phys := sc.netPhysical
	virt := sc.netVirtual
	sc.mu.RUnlock()

	mem := readMemory()
	disks := readDisks()
	temp, tempAvail := readTemperature()
	uptime := readUptime()

	netReport := NetworkReport{Physical: phys}
	if virt.DownloadBytesPerSec > 0 || virt.UploadBytesPerSec > 0 ||
		virt.RxErrors > 0 || virt.TxErrors > 0 || virt.RxDrops > 0 || virt.TxDrops > 0 {
		netReport.Virtual = &virt
	}

	return SystemStats{
		CPU: CPUStats{
			UsagePercent:         cpuUsage,
			CoreCount:            sc.coreCount,
			Temperature:          temp,
			TemperatureAvailable: tempAvail,
		},
		Memory:  mem,
		Disks:   disks,
		Network: netReport,
		Uptime:  uptime,
	}
}

// CollectTopProcesses returns the pre-calculated top processes by CPU usage.
func (sc *SystemCollector) CollectTopProcesses(limit int) []ProcessInfo {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	if limit > len(sc.topProcesses) {
		limit = len(sc.topProcesses)
	}
	result := make([]ProcessInfo, limit)
	copy(result, sc.topProcesses[:limit])
	return result
}

// sampleProcesses reads /proc/ to collect per-process CPU and memory stats.
// Must be called with sc.mu held for writing.
func (sc *SystemCollector) sampleProcesses() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}

	now := time.Now()
	clkTck := float64(100) // standard Linux USER_HZ
	numCPU := sc.coreCount
	if numCPU < 1 {
		numCPU = 1
	}

	totalMemKB := sc.totalMemKB
	if totalMemKB == 0 {
		totalMemKB = readTotalMemKB()
		sc.totalMemKB = totalMemKB
	}

	currentPIDs := make(map[int32]struct{})
	var processes []ProcessInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid64, err := strconv.ParseInt(entry.Name(), 10, 32)
		if err != nil {
			continue
		}
		pid := int32(pid64)
		currentPIDs[pid] = struct{}{}

		procDir := filepath.Join("/proc", entry.Name())

		// Read CPU times from /proc/<pid>/stat
		name, utime, stime, ok := readProcStat(procDir)
		if !ok {
			continue
		}

		// Read RSS from /proc/<pid>/status
		rssKB := readProcRSS(procDir)

		// Calculate CPU percent as delta from previous sample
		var rawCPU float64
		if prev, exists := sc.prevProcCPU[pid]; exists {
			elapsed := now.Sub(prev.timestamp).Seconds()
			if elapsed > 0 {
				totalTicks := float64((utime - prev.utime) + (stime - prev.stime))
				rawCPU = (totalTicks / clkTck) / elapsed * 100.0 / float64(numCPU)
				if rawCPU < 0 {
					rawCPU = 0
				}
			}
		}

		// EMA smoothing (alpha=0.2) to prevent noisy per-second fluctuations.
		// Lower alpha = heavier smoothing = more stable rankings.
		const emaAlpha = 0.2
		cpuPercent := rawCPU
		if prev, exists := sc.smoothedCPU[pid]; exists {
			cpuPercent = emaAlpha*rawCPU + (1-emaAlpha)*prev
		}
		sc.smoothedCPU[pid] = cpuPercent
		cpuPercent = math.Round(cpuPercent*100) / 100

		// Update previous sample
		sc.prevProcCPU[pid] = processCPUSample{
			utime:     utime,
			stime:     stime,
			timestamp: now,
		}

		memMB := float64(rssKB) / 1024.0
		memMB = math.Round(memMB*100) / 100

		var memPercent float64
		if totalMemKB > 0 {
			memPercent = float64(rssKB) / float64(totalMemKB) * 100.0
			memPercent = math.Round(memPercent*100) / 100
		}

		processes = append(processes, ProcessInfo{
			PID:           pid,
			Name:          name,
			CPUPercent:    cpuPercent,
			MemoryMB:      memMB,
			MemoryPercent: memPercent,
		})
	}

	// Clean up stale entries for dead processes
	for pid := range sc.prevProcCPU {
		if _, alive := currentPIDs[pid]; !alive {
			delete(sc.prevProcCPU, pid)
			delete(sc.smoothedCPU, pid)
		}
	}

	// Combined score: CPU usage + memory weight so heavy-RAM processes stay visible.
	// 200 MB ≈ 20 score points, comparable to 20% CPU.
	procScore := func(p ProcessInfo) float64 {
		return p.CPUPercent + p.MemoryMB*0.1
	}

	// Sort by combined score descending; use PID as tiebreaker for stability.
	// Threshold of 0.5 prevents tiny fluctuations from shuffling the list.
	sort.Slice(processes, func(i, j int) bool {
		si, sj := procScore(processes[i]), procScore(processes[j])
		if math.Abs(si-sj) < 0.5 {
			return processes[i].PID < processes[j].PID
		}
		return si > sj
	})

	// Keep top 15 — frontend caps display at 10 but uses the extra
	// headroom for its damped sort to prevent processes popping in/out.
	const maxKeep = 15
	if len(processes) > maxKeep {
		processes = processes[:maxKeep]
	}

	// Enrich top processes with command line and user (only for top N to avoid excess I/O)
	for i := range processes {
		procDir := filepath.Join("/proc", strconv.Itoa(int(processes[i].PID)))
		processes[i].Command = readProcCmdline(procDir)
		processes[i].User = readProcUser(procDir)
	}

	sc.topProcesses = processes
}

// readProcStat reads /proc/<pid>/stat and returns the process name and CPU times.
// Returns (name, utime, stime, ok).
func readProcStat(procDir string) (string, uint64, uint64, bool) {
	data, err := os.ReadFile(filepath.Join(procDir, "stat"))
	if err != nil {
		return "", 0, 0, false
	}

	content := string(data)

	// The process name is in parentheses and can contain spaces and special chars.
	// Find the last ')' to handle names like "(my process)" correctly.
	openParen := strings.IndexByte(content, '(')
	closeParen := strings.LastIndexByte(content, ')')
	if openParen < 0 || closeParen < 0 || closeParen <= openParen {
		return "", 0, 0, false
	}

	name := content[openParen+1 : closeParen]

	// Fields after the closing paren: state, ppid, pgrp, session, tty_nr,
	// tpgid, flags, minflt, cminflt, majflt, cmajflt, utime(14), stime(15)
	// Index relative to after ')': field 0=state, ..., field 11=utime, field 12=stime
	rest := strings.TrimSpace(content[closeParen+1:])
	fields := strings.Fields(rest)
	// utime is at index 11 (field 14 overall, minus 3 for pid/name/state offset)
	// Actually: fields after ')' are indexed from 0.
	// field 0 = state (field 3 overall)
	// field 11 = utime (field 14 overall)
	// field 12 = stime (field 15 overall)
	if len(fields) < 13 {
		return "", 0, 0, false
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return "", 0, 0, false
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return "", 0, 0, false
	}

	return name, utime, stime, true
}

// readProcRSS reads VmRSS from /proc/<pid>/status and returns the value in kB.
func readProcRSS(procDir string) uint64 {
	f, err := os.Open(filepath.Join(procDir, "status"))
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			return parseMemInfoValue(line)
		}
	}
	return 0
}

// readTotalMemKB reads the total system memory in kB from /proc/meminfo.
func readTotalMemKB() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			return parseMemInfoValue(line)
		}
	}
	return 0
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

// isVirtualInterface returns true for docker bridges, veth pairs, and similar virtual interfaces.
func isVirtualInterface(name string) bool {
	if strings.HasPrefix(name, "docker") ||
		strings.HasPrefix(name, "br-") ||
		strings.HasPrefix(name, "veth") ||
		strings.HasPrefix(name, "virbr") ||
		strings.HasPrefix(name, "lxc") ||
		strings.HasPrefix(name, "flannel") ||
		strings.HasPrefix(name, "cni") ||
		strings.HasPrefix(name, "cali") {
		return true
	}
	return false
}

func readNetSample() netSample {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return netSample{timestamp: time.Now()}
	}
	defer f.Close()

	var s netSample
	s.timestamp = time.Now()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "|") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 12 {
			continue
		}

		// /proc/net/dev columns:
		// RX: bytes(0) packets(1) errs(2) drop(3) ...
		// TX: bytes(8) packets(9) errs(10) drop(11) ...
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		rxErr, _ := strconv.ParseUint(fields[2], 10, 64)
		rxDrop, _ := strconv.ParseUint(fields[3], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		txErr, _ := strconv.ParseUint(fields[10], 10, 64)
		txDrop, _ := strconv.ParseUint(fields[11], 10, 64)

		if isVirtualInterface(iface) {
			s.virtRx += rx
			s.virtTx += tx
			s.virtRxErr += rxErr
			s.virtRxDrop += rxDrop
			s.virtTxErr += txErr
			s.virtTxDrop += txDrop
		} else {
			s.physRx += rx
			s.physTx += tx
			s.physRxErr += rxErr
			s.physRxDrop += rxDrop
			s.physTxErr += txErr
			s.physTxDrop += txDrop
		}
	}

	return s
}

func calcInterfaceStats(
	prevRx, curRx, prevTx, curTx uint64,
	prevRxErr, curRxErr, prevRxDrop, curRxDrop uint64,
	prevTxErr, curTxErr, prevTxDrop, curTxDrop uint64,
	elapsed float64,
) InterfaceStats {
	dl := float64(curRx-prevRx) / elapsed
	ul := float64(curTx-prevTx) / elapsed
	return InterfaceStats{
		DownloadBytesPerSec: math.Round(dl*100) / 100,
		UploadBytesPerSec:   math.Round(ul*100) / 100,
		RxErrors:            curRxErr - prevRxErr,
		RxDrops:             curRxDrop - prevRxDrop,
		TxErrors:            curTxErr - prevTxErr,
		TxDrops:             curTxDrop - prevTxDrop,
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

// pseudoFS is the set of filesystem types to skip when enumerating disks.
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "sysfs": true, "proc": true,
	"cgroup": true, "cgroup2": true, "overlay": true, "nsfs": true,
	"fuse.lxcfs": true, "squashfs": true, "devpts": true, "securityfs": true,
	"pstore": true, "efivarfs": true, "bpf": true, "tracefs": true,
	"debugfs": true, "hugetlbfs": true, "mqueue": true, "configfs": true,
	"fusectl": true, "autofs": true, "ramfs": true, "rpc_pipefs": true,
	"nfsd": true, "fuse.gvfsd-fuse": true,
}

func readDisks() []DiskInfo {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()

	seenDevices := make(map[string]bool)
	var disks []DiskInfo

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		// Skip pseudo-filesystems
		if pseudoFS[fsType] {
			continue
		}
		// Skip non-device mounts (e.g., "none", "systemd-1")
		if !strings.HasPrefix(device, "/") {
			continue
		}
		// Deduplicate by device (keep first mount)
		if seenDevices[device] {
			continue
		}
		seenDevices[device] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}

		totalBytes := stat.Blocks * uint64(stat.Bsize)
		availBytes := stat.Bavail * uint64(stat.Bsize) // user-available (excludes reserved)
		usedBytes := totalBytes - (stat.Bfree * uint64(stat.Bsize))

		// Skip tiny/empty filesystems (< 50MB)
		if totalBytes < 50*1024*1024 {
			continue
		}

		_ = availBytes // Bavail used for correct "available" calculation
		disks = append(disks, DiskInfo{
			MountPoint: mountPoint,
			Device:     device,
			FsType:     fsType,
			UsedBytes:  usedBytes,
			TotalBytes: totalBytes,
		})
	}

	// Sort by mount point for stable ordering
	sort.Slice(disks, func(i, j int) bool {
		return disks[i].MountPoint < disks[j].MountPoint
	})

	return disks
}

func readTemperature() (float64, bool) {
	matches, err := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	if err != nil || len(matches) == 0 {
		return 0, false
	}

	var maxTemp float64
	found := false
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		temp := val / 1000.0
		if temp > 0 {
			found = true
			if temp > maxTemp {
				maxTemp = temp
			}
		}
	}

	return math.Round(maxTemp*10) / 10, found
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

// readProcCmdline reads /proc/<pid>/cmdline and returns the full command line.
func readProcCmdline(procDir string) string {
	data, err := os.ReadFile(filepath.Join(procDir, "cmdline"))
	if err != nil || len(data) == 0 {
		return ""
	}
	// cmdline is null-byte separated
	cmdline := strings.ReplaceAll(string(data), "\x00", " ")
	cmdline = strings.TrimSpace(cmdline)
	if len(cmdline) > 256 {
		cmdline = cmdline[:256]
	}
	return cmdline
}

// readProcUser reads the effective UID from /proc/<pid>/status and resolves it to a username.
func readProcUser(procDir string) string {
	f, err := os.Open(filepath.Join(procDir, "status"))
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				u, err := user.LookupId(fields[1])
				if err == nil {
					return u.Username
				}
				return fields[1]
			}
		}
	}
	return ""
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
