package gpu

import (
	"encoding/xml"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Monitor provides GPU monitoring functionality
type Monitor struct{}

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
	// Check if nvidia-smi is available
	_, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi not found: %w", err)
	}

	return &Monitor{}, nil
}

// GetGPUInfo returns information about the GPU
func (m *Monitor) GetGPUInfo() (*GPUInfo, error) {
	// Run nvidia-smi command
	cmd := exec.Command("nvidia-smi", "-x", "-q")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvidia-smi: %w", err)
	}

	// Parse XML output
	var nvidiaSMI NvidiaSMI
	if err := xml.Unmarshal(output, &nvidiaSMI); err != nil {
		return nil, fmt.Errorf("failed to parse nvidia-smi output: %w", err)
	}

	// Extract GPU count
	gpuCount, err := strconv.Atoi(nvidiaSMI.AttachedGPUs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GPU count: %w", err)
	}

	// If no GPUs are found, return an error
	if gpuCount == 0 || len(nvidiaSMI.GPUs) == 0 {
		return nil, fmt.Errorf("no GPUs found")
	}

	// Use the first GPU for now
	gpu := nvidiaSMI.GPUs[0]

	// Parse memory values
	memTotal, _ := parseMemoryValue(gpu.FBMemoryUsage.Total)
	memUsed, _ := parseMemoryValue(gpu.FBMemoryUsage.Used)
	memFree, _ := parseMemoryValue(gpu.FBMemoryUsage.Free)

	// Parse utilization
	utilization, _ := parseUtilization(gpu.Utilization.GPUUtil)

	// Determine if GPU is busy (utilization > 20%)
	isBusy := utilization > 20.0

	return &GPUInfo{
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
	}, nil
}

// IsBusy returns true if the GPU is busy
func (m *Monitor) IsBusy() (bool, error) {
	info, err := m.GetGPUInfo()
	if err != nil {
		return false, err
	}

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
