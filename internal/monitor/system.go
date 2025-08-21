package monitor

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/cctv-agent/internal/logger"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// SystemMonitor monitors system resources
type SystemMonitor struct {
	logger logger.Logger
}

// SystemStats represents system statistics
type SystemStats struct {
	CPUUsage    float64
	MemoryUsage float64
	DiskUsage   float64
	Temperature float64
	Network     NetworkStats
}

// NetworkStats represents network statistics
type NetworkStats struct {
	BytesSent       uint64
	BytesReceived   uint64
	PacketsSent     uint64
	PacketsReceived uint64
}

// NewSystemMonitor creates a new system monitor
func NewSystemMonitor(log logger.Logger) *SystemMonitor {
	return &SystemMonitor{
		logger: log,
	}
}

// GetSystemStats returns current system statistics
func (m *SystemMonitor) GetSystemStats() (*SystemStats, error) {
	stats := &SystemStats{}
	
	// Get CPU usage
	cpuPercent, err := cpu.Percent(1000, false)
	if err == nil && len(cpuPercent) > 0 {
		stats.CPUUsage = cpuPercent[0]
	}
	
	// Get memory usage
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		stats.MemoryUsage = memInfo.UsedPercent
	}
	
	// Get disk usage
	diskInfo, err := disk.Usage("/")
	if err == nil {
		stats.DiskUsage = diskInfo.UsedPercent
	}
	
	// Get temperature (Raspberry Pi specific)
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm" {
		stats.Temperature = m.getRaspberryPiTemperature()
	}
	
	// Get network stats
	netStats, err := net.IOCounters(false)
	if err == nil && len(netStats) > 0 {
		stats.Network = NetworkStats{
			BytesSent:       netStats[0].BytesSent,
			BytesReceived:   netStats[0].BytesRecv,
			PacketsSent:     netStats[0].PacketsSent,
			PacketsReceived: netStats[0].PacketsRecv,
		}
	}
	
	return stats, nil
}

// getRaspberryPiTemperature gets CPU temperature on Raspberry Pi
func (m *SystemMonitor) getRaspberryPiTemperature() float64 {
	// Try to read from thermal zone
	cmd := exec.Command("cat", "/sys/class/thermal/thermal_zone0/temp")
	output, err := cmd.Output()
	if err != nil {
		// Try vcgencmd as fallback
		cmd = exec.Command("vcgencmd", "measure_temp")
		output, err = cmd.Output()
		if err != nil {
			m.logger.Debug("Failed to get temperature", "error", err)
			return 0
		}
		
		// Parse vcgencmd output: temp=42.8'C
		tempStr := string(output)
		if strings.Contains(tempStr, "temp=") {
			tempStr = strings.TrimPrefix(tempStr, "temp=")
			tempStr = strings.TrimSuffix(tempStr, "'C\n")
			temp, err := strconv.ParseFloat(tempStr, 64)
			if err == nil {
				return temp
			}
		}
		return 0
	}
	
	// Parse thermal zone output (millidegrees)
	tempStr := strings.TrimSpace(string(output))
	temp, err := strconv.ParseFloat(tempStr, 64)
	if err != nil {
		return 0
	}
	
	return temp / 1000.0
}

// GetCPUUsage returns current CPU usage percentage
func (m *SystemMonitor) GetCPUUsage() (float64, error) {
	cpuPercent, err := cpu.Percent(1000, false)
	if err != nil {
		return 0, err
	}
	
	if len(cpuPercent) == 0 {
		return 0, nil
	}
	
	return cpuPercent[0], nil
}

// GetMemoryUsage returns current memory usage percentage
func (m *SystemMonitor) GetMemoryUsage() (float64, error) {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	
	return memInfo.UsedPercent, nil
}

// GetDiskUsage returns disk usage percentage for the root partition
func (m *SystemMonitor) GetDiskUsage() (float64, error) {
	diskInfo, err := disk.Usage("/")
	if err != nil {
		return 0, err
	}
	
	return diskInfo.UsedPercent, nil
}

// GetNetworkStats returns network statistics
func (m *SystemMonitor) GetNetworkStats() (*NetworkStats, error) {
	netStats, err := net.IOCounters(false)
	if err != nil {
		return nil, err
	}
	
	if len(netStats) == 0 {
		return &NetworkStats{}, nil
	}
	
	return &NetworkStats{
		BytesSent:       netStats[0].BytesSent,
		BytesReceived:   netStats[0].BytesRecv,
		PacketsSent:     netStats[0].PacketsSent,
		PacketsReceived: netStats[0].PacketsRecv,
	}, nil
}
