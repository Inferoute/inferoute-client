package gpu

import (
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/exechide"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

// gpuInfoCacheTTL is long enough to collapse a request flood into one
// nvidia-smi, short enough that busy detection stays useful.
const gpuInfoCacheTTL = time.Second

// nvidiaSMITimeout bounds nvidia-smi / system_profiler so a hung driver
// cannot pin every GetGPUInfo caller behind the cache mutex.
const nvidiaSMITimeout = 5 * time.Second

// Monitor provides GPU monitoring functionality
type Monitor struct {
	isMacOS bool

	mu        sync.Mutex
	cached    *GPUInfo
	cachedErr error
	cachedAt  time.Time
	cacheTTL  time.Duration
	queryFn   func() (*GPUInfo, error) // tests; nil uses nvidia-smi / system_profiler

	macName string // system_profiler is slow and the chip never changes
}

// GPUInfo represents information about the GPU
type GPUInfo struct {
	ProductName   string  `json:"product_name"`
	DriverVersion string  `json:"driver_version"`
	CUDAVersion   string  `json:"cuda_version"`
	GPUCount      int     `json:"gpu_count"`
	UUID          string  `json:"uuid"`
	Utilization   float64 `json:"utilization"`
	MemoryTotal   int64   `json:"memory_total"`
	MemoryUsed    int64   `json:"memory_used"`
	MemoryFree    int64   `json:"memory_free"`
	IsBusy        bool    `json:"is_busy"`
	// Zero means the platform did not report it.
	TemperatureC float64 `json:"temperature_c,omitempty"`
	PowerDrawW   float64 `json:"power_draw_w,omitempty"`
	PowerLimitW  float64 `json:"power_limit_w,omitempty"`
}

// NvidiaSMI represents the XML output of nvidia-smi
type NvidiaSMI struct {
	XMLName       xml.Name `xml:"nvidia_smi_log"`
	DriverVersion string   `xml:"driver_version"`
	CUDAVersion   string   `xml:"cuda_version"`
	AttachedGPUs  string   `xml:"attached_gpus"`
	GPUs          []GPU    `xml:"gpu"`
}

// GPU represents a single GPU in the nvidia-smi output
type GPU struct {
	ID            string      `xml:"id,attr"`
	ProductName   string      `xml:"product_name"`
	UUID          string      `xml:"uuid"`
	FBMemoryUsage MemoryUsage `xml:"fb_memory_usage"`
	Utilization   Utilization `xml:"utilization"`
	Temperature   struct {
		GPUTemp string `xml:"gpu_temp"`
	} `xml:"temperature"`
	// Drivers before ~R530 use power_readings; newer ones gpu_power_readings.
	PowerReadings    PowerReadings `xml:"power_readings"`
	GPUPowerReadings PowerReadings `xml:"gpu_power_readings"`
}

// PowerReadings covers the field names used across driver versions.
type PowerReadings struct {
	PowerDraw         string `xml:"power_draw"`
	InstantPowerDraw  string `xml:"instant_power_draw"`
	AveragePowerDraw  string `xml:"average_power_draw"`
	CurrentPowerLimit string `xml:"current_power_limit"`
	PowerLimit        string `xml:"power_limit"`
}

// power returns draw and limit in watts, zero when not reported.
func (g GPU) power() (draw, limit float64) {
	for _, r := range []PowerReadings{g.GPUPowerReadings, g.PowerReadings} {
		if draw == 0 {
			draw = firstUnitValue(r.InstantPowerDraw, r.PowerDraw, r.AveragePowerDraw)
		}
		if limit == 0 {
			limit = firstUnitValue(r.CurrentPowerLimit, r.PowerLimit)
		}
	}
	return draw, limit
}

// firstUnitValue parses the first positive value like "120.50 W" or "45 C";
// "N/A" and blanks are skipped.
func firstUnitValue(values ...string) float64 {
	for _, v := range values {
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		if n, err := strconv.ParseFloat(f[0], 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// MemoryUsage represents memory usage information
type MemoryUsage struct {
	Total string `xml:"total"`
	Used  string `xml:"used"`
	Free  string `xml:"free"`
}

// Utilization represents GPU utilization information
type Utilization struct {
	GPUUtil    string `xml:"gpu_util"`
	MemoryUtil string `xml:"memory_util"`
}

// NewMonitor creates a new GPU monitor
func NewMonitor() (*Monitor, error) {
	isMacOS := runtime.GOOS == "darwin"

	if !isMacOS {
		// Check if nvidia-smi is available on non-macOS systems
		_, err := exec.LookPath("nvidia-smi")
		if err != nil {
			logger.Error("nvidia-smi not found", zap.Error(err))
			return nil, fmt.Errorf("nvidia-smi not found: %w", err)
		}
	} else {
		// Check if system_profiler is available on macOS
		_, err := exec.LookPath("system_profiler")
		if err != nil {
			logger.Error("system_profiler not found", zap.Error(err))
			return nil, fmt.Errorf("system_profiler not found: %w", err)
		}
	}

	logger.Debug("GPU monitor initialized successfully", zap.Bool("is_macos", isMacOS))
	return &Monitor{isMacOS: isMacOS, cacheTTL: gpuInfoCacheTTL}, nil
}

// GetGPUInfo returns information about the GPU. Results are cached for
// gpuInfoCacheTTL; callers receive a copy so mutations do not poison the cache.
func (m *Monitor) GetGPUInfo() (*GPUInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ttl := m.cacheTTL
	if ttl <= 0 {
		ttl = gpuInfoCacheTTL
	}
	if !m.cachedAt.IsZero() && time.Since(m.cachedAt) < ttl {
		if m.cachedErr != nil {
			return nil, m.cachedErr
		}
		if m.cached != nil {
			info := *m.cached
			return &info, nil
		}
	}

	info, err := m.queryGPUInfo()
	m.cachedAt = time.Now()
	m.cachedErr = err
	if err != nil {
		m.cached = nil
		return nil, err
	}
	m.cached = info
	out := *info
	return &out, nil
}

func (m *Monitor) queryGPUInfo() (*GPUInfo, error) {
	if m.queryFn != nil {
		return m.queryFn()
	}
	if m.isMacOS {
		return m.getMacGPUInfo()
	}
	return m.getNvidiaGPUInfo()
}

func (m *Monitor) getNvidiaGPUInfo() (*GPUInfo, error) {
	logger.Debug("Getting GPU information")

	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSMITimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi", "-x", "-q")
	exechide.Apply(cmd)
	output, err := cmd.Output()
	if err != nil {
		logger.Error("Failed to run nvidia-smi", zap.Error(err))
		return nil, fmt.Errorf("failed to run nvidia-smi: %w", err)
	}

	// Parse XML output
	var nvidiaSMI NvidiaSMI
	if err := xml.Unmarshal(output, &nvidiaSMI); err != nil {
		logger.Error("Failed to parse nvidia-smi output", zap.Error(err))
		return nil, fmt.Errorf("failed to parse nvidia-smi output: %w", err)
	}

	// Extract GPU count
	gpuCount, err := strconv.Atoi(nvidiaSMI.AttachedGPUs)
	if err != nil {
		logger.Error("Failed to parse GPU count", zap.Error(err), zap.String("attached_gpus", nvidiaSMI.AttachedGPUs))
		return nil, fmt.Errorf("failed to parse GPU count: %w", err)
	}

	// If no GPUs are found, return an error
	if gpuCount == 0 || len(nvidiaSMI.GPUs) == 0 {
		logger.Error("No GPUs found", zap.Int("gpu_count", gpuCount), zap.Int("gpus_length", len(nvidiaSMI.GPUs)))
		return nil, fmt.Errorf("no GPUs found")
	}

	// Use the first GPU for now
	gpu := nvidiaSMI.GPUs[0]

	// Parse memory values
	memTotal, err := parseMemoryValue(gpu.FBMemoryUsage.Total)
	if err != nil {
		logger.Warn("Failed to parse memory total", zap.Error(err), zap.String("memory_total", gpu.FBMemoryUsage.Total))
	}

	memUsed, err := parseMemoryValue(gpu.FBMemoryUsage.Used)
	if err != nil {
		logger.Warn("Failed to parse memory used", zap.Error(err), zap.String("memory_used", gpu.FBMemoryUsage.Used))
	}

	memFree, err := parseMemoryValue(gpu.FBMemoryUsage.Free)
	if err != nil {
		logger.Warn("Failed to parse memory free", zap.Error(err), zap.String("memory_free", gpu.FBMemoryUsage.Free))
	}

	// Parse utilization
	utilization, err := parseUtilization(gpu.Utilization.GPUUtil)
	if err != nil {
		logger.Warn("Failed to parse GPU utilization", zap.Error(err), zap.String("gpu_util", gpu.Utilization.GPUUtil))
	}

	// Determine if GPU is busy (utilization > 20%)
	isBusy := utilization > 20.0
	powerDraw, powerLimit := gpu.power()

	gpuInfo := &GPUInfo{
		ProductName:   gpu.ProductName,
		DriverVersion: nvidiaSMI.DriverVersion,
		CUDAVersion:   nvidiaSMI.CUDAVersion,
		GPUCount:      gpuCount,
		UUID:          gpu.UUID,
		Utilization:   utilization,
		MemoryTotal:   memTotal,
		MemoryUsed:    memUsed,
		MemoryFree:    memFree,
		IsBusy:        isBusy,
		TemperatureC:  firstUnitValue(gpu.Temperature.GPUTemp),
		PowerDrawW:    powerDraw,
		PowerLimitW:   powerLimit,
	}

	logger.Debug("GPU information retrieved successfully",
		zap.String("product_name", gpuInfo.ProductName),
		zap.String("driver_version", gpuInfo.DriverVersion),
		zap.Float64("utilization", gpuInfo.Utilization),
		zap.Bool("is_busy", gpuInfo.IsBusy),
		zap.Float64("temperature_c", gpuInfo.TemperatureC),
		zap.Float64("power_draw_w", gpuInfo.PowerDrawW),
		zap.Float64("power_limit_w", gpuInfo.PowerLimitW))

	return gpuInfo, nil
}

// getMacGPUInfo returns information about the GPU on macOS
func (m *Monitor) getMacGPUInfo() (*GPUInfo, error) {
	logger.Debug("Getting macOS GPU information")

	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSMITimeout)
	defer cancel()

	name := m.macName
	if name == "" {
		got, err := macProductName(ctx)
		if err != nil {
			return nil, err
		}
		name = got
		m.macName = got
	}

	stats := queryMacStats(ctx)
	powerW, tempC := macSensors()
	const mib = 1 << 20
	used := stats.UsedBytes / mib
	total := stats.TotalBytes / mib
	free := total - used
	if free < 0 {
		free = 0
	}

	gpuInfo := &GPUInfo{
		ProductName:   name,
		DriverVersion: "macOS Native",
		CUDAVersion:   "N/A",
		GPUCount:      1,
		UUID:          "mac-gpu",
		Utilization:   stats.Utilization,
		MemoryTotal:   total,
		MemoryUsed:    used,
		MemoryFree:    free,
		IsBusy:        stats.Utilization > 20.0,
		TemperatureC:  tempC,
		PowerDrawW:    powerW,
	}

	logger.Debug("macOS GPU information retrieved successfully",
		zap.String("product_name", gpuInfo.ProductName),
		zap.Float64("utilization", gpuInfo.Utilization),
		zap.Int64("memory_used_mib", gpuInfo.MemoryUsed),
		zap.Int64("memory_total_mib", gpuInfo.MemoryTotal),
		zap.Float64("temperature_c", gpuInfo.TemperatureC),
		zap.Float64("power_draw_w", gpuInfo.PowerDrawW))

	return gpuInfo, nil
}

// macProductName returns the chip name (with core count) from system_profiler.
func macProductName(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "system_profiler", "SPDisplaysDataType")
	output, err := cmd.Output()
	if err != nil {
		logger.Error("Failed to run system_profiler", zap.Error(err))
		return "", fmt.Errorf("failed to run system_profiler: %w", err)
	}

	// Parse the output
	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	var productName string
	var coreCount int

	// Parse the output to extract GPU information
	for i, line := range lines {
		line = strings.TrimSpace(line)

		// Look for the GPU model line (usually starts with "Chipset Model:")
		if strings.Contains(line, "Chipset Model:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				productName = strings.TrimSpace(parts[1])
			}
		}

		// Look for the core count line (usually "Total Number of Cores:")
		if strings.Contains(line, "Total Number of Cores:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				countStr := strings.TrimSpace(parts[1])
				count, err := strconv.Atoi(countStr)
				if err == nil {
					coreCount = count
				}
			}
		}

		// If we found a GPU name but didn't find it in the previous iteration,
		// look at the previous line to get the GPU name from the section header
		if productName == "" && i > 0 && strings.HasSuffix(lines[i-1], ":") {
			productName = strings.TrimSuffix(strings.TrimSpace(lines[i-1]), ":")
		}
	}

	// If we couldn't find a product name, use a default
	if productName == "" {
		productName = "Unknown macOS GPU"
	}

	if coreCount > 0 {
		return fmt.Sprintf("%s (%d cores)", productName, coreCount), nil
	}
	return productName, nil
}

// IsBusy returns true if the GPU is busy
func (m *Monitor) IsBusy() (bool, error) {
	logger.Debug("Checking if GPU is busy")

	info, err := m.GetGPUInfo()
	if err != nil {
		logger.Error("Failed to check if GPU is busy", zap.Error(err))
		return false, err
	}

	logger.Debug("GPU busy status", zap.Bool("is_busy", info.IsBusy), zap.Float64("utilization", info.Utilization))
	return info.IsBusy, nil
}

// parseMemoryValue parses a memory value string (e.g., "1234 MiB")
func parseMemoryValue(value string) (int64, error) {
	parts := strings.Split(value, " ")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid memory value format: %s", value)
	}

	// Parse numeric part
	numValue, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory value: %w", err)
	}

	return numValue, nil
}

// parseUtilization parses a utilization value string (e.g., "50 %")
func parseUtilization(value string) (float64, error) {
	parts := strings.Split(value, " ")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid utilization format: %s", value)
	}

	// Parse numeric part
	numValue, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse utilization: %w", err)
	}

	return numValue, nil
}
