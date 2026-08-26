package compat

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// MemoryKind describes which memory pool the scorer should use.
type MemoryKind string

const (
	MemoryVRAM    MemoryKind = "vram"
	MemoryUnified MemoryKind = "unified"
	MemorySystem  MemoryKind = "system"
)

// Hardware is the local machine snapshot used for compatibility scoring.
type Hardware struct {
	OS               string     `json:"os"`
	Arch             string     `json:"arch"`
	ProductName      string     `json:"product_name"`
	DriverVersion    string     `json:"driver_version,omitempty"`
	CUDAVersion      string     `json:"cuda_version,omitempty"`
	GPUCount         int        `json:"gpu_count"`
	MemoryKind       MemoryKind `json:"memory_kind"`
	MemoryTotalBytes int64      `json:"memory_total_bytes"`
	MemoryFreeBytes  int64      `json:"memory_free_bytes"`
	MemoryUsedBytes  int64      `json:"memory_used_bytes"`
	SystemRAMBytes   int64      `json:"system_ram_bytes"`
	UsableBytes      int64      `json:"usable_bytes"`
	UnifiedMemory    bool       `json:"unified_memory"`
	Warnings         []string   `json:"warnings,omitempty"`
}

// Detect probes local hardware for Linux/Windows NVIDIA and macOS (Apple Silicon/Intel).
// It never hard-fails solely because a GPU is missing; CPU/system-RAM fallbacks are used.
func Detect() (*Hardware, error) {
	hw := &Hardware{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	ram, err := systemRAMBytes()
	if err != nil {
		hw.Warnings = append(hw.Warnings, fmt.Sprintf("system RAM detection failed: %v", err))
	} else {
		hw.SystemRAMBytes = ram
	}

	switch runtime.GOOS {
	case "darwin":
		if err := detectDarwin(hw); err != nil {
			hw.Warnings = append(hw.Warnings, err.Error())
			fallbackSystemMemory(hw, "macOS GPU detection failed; using system RAM")
		}
	case "linux", "windows":
		if err := detectNVIDIA(hw); err != nil {
			hw.Warnings = append(hw.Warnings, err.Error())
			fallbackSystemMemory(hw, "NVIDIA GPU not detected; scoring against system RAM (slow/CPU path)")
		}
	default:
		fallbackSystemMemory(hw, fmt.Sprintf("unsupported OS %s; using system RAM if available", runtime.GOOS))
	}

	if hw.UsableBytes <= 0 && hw.SystemRAMBytes > 0 {
		fallbackSystemMemory(hw, "usable memory unset; falling back to system RAM")
	}

	return hw, nil
}

func fallbackSystemMemory(hw *Hardware, warning string) {
	if warning != "" {
		hw.Warnings = append(hw.Warnings, warning)
	}
	if hw.ProductName == "" {
		hw.ProductName = "CPU / unknown GPU"
	}
	hw.MemoryKind = MemorySystem
	hw.UnifiedMemory = false
	hw.MemoryTotalBytes = hw.SystemRAMBytes
	hw.MemoryFreeBytes = 0
	hw.MemoryUsedBytes = 0
	// Leave headroom for OS and other processes.
	hw.UsableBytes = int64(float64(hw.SystemRAMBytes) * 0.70)
	if hw.GPUCount == 0 {
		hw.GPUCount = 0
	}
}

// appleSiliconUsableFraction is a conservative share of unified memory for model weights + runtime.
const appleSiliconUsableFraction = 0.65

func detectDarwin(hw *Hardware) error {
	productName, coreCount, err := parseSystemProfilerGPU()
	if err != nil {
		return err
	}
	hw.ProductName = productName
	if coreCount > 0 {
		hw.ProductName = fmt.Sprintf("%s (%d cores)", productName, coreCount)
	}
	hw.DriverVersion = "macOS Native"
	hw.CUDAVersion = "N/A"
	hw.GPUCount = 1

	isAppleSilicon := runtime.GOARCH == "arm64" ||
		strings.Contains(strings.ToLower(productName), "apple")

	if isAppleSilicon {
		hw.MemoryKind = MemoryUnified
		hw.UnifiedMemory = true
		hw.MemoryTotalBytes = hw.SystemRAMBytes
		hw.UsableBytes = int64(float64(hw.SystemRAMBytes) * appleSiliconUsableFraction)
		if hw.SystemRAMBytes <= 0 {
			return fmt.Errorf("Apple Silicon detected but system RAM is unknown")
		}
		return nil
	}

	// Intel Mac: discrete/shared GPU memory is unreliable via system_profiler; use system RAM.
	fallbackSystemMemory(hw, "Intel Mac GPU VRAM not exposed; scoring against system RAM")
	hw.ProductName = productName
	return nil
}

func parseSystemProfilerGPU() (productName string, coreCount int, err error) {
	if _, lookErr := exec.LookPath("system_profiler"); lookErr != nil {
		return "", 0, fmt.Errorf("system_profiler not found: %w", lookErr)
	}
	cmd := exec.Command("system_profiler", "SPDisplaysDataType")
	output, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("system_profiler: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Chipset Model:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				productName = strings.TrimSpace(parts[1])
			}
		}
		if strings.Contains(line, "Total Number of Cores:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if n, atoiErr := strconv.Atoi(strings.TrimSpace(parts[1])); atoiErr == nil {
					coreCount = n
				}
			}
		}
		if productName == "" && i > 0 && strings.HasSuffix(strings.TrimSpace(lines[i-1]), ":") {
			productName = strings.TrimSuffix(strings.TrimSpace(lines[i-1]), ":")
		}
	}
	if productName == "" {
		productName = "Unknown macOS GPU"
	}
	return productName, coreCount, nil
}

func detectNVIDIA(hw *Hardware) error {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return fmt.Errorf("nvidia-smi not found: %w", err)
	}
	cmd := exec.Command("nvidia-smi", "--query-gpu=name,driver_version,memory.total,memory.used,memory.free,uuid", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("nvidia-smi: %w", err)
	}

	cudaVersion := queryCUDAVersion()

	type gpuRow struct {
		name   string
		driver string
		total  int64
		used   int64
		free   int64
	}
	var gpus []gpuRow
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := splitCSV(line)
		if len(parts) < 5 {
			continue
		}
		totalMiB, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		usedMiB, _ := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
		freeMiB, _ := strconv.ParseInt(strings.TrimSpace(parts[4]), 10, 64)
		gpus = append(gpus, gpuRow{
			name:   strings.TrimSpace(parts[0]),
			driver: strings.TrimSpace(parts[1]),
			total:  totalMiB * 1024 * 1024,
			used:   usedMiB * 1024 * 1024,
			free:   freeMiB * 1024 * 1024,
		})
	}
	if len(gpus) == 0 {
		return fmt.Errorf("nvidia-smi returned no GPUs")
	}

	// v1: score against the largest single GPU VRAM.
	best := gpus[0]
	for _, g := range gpus[1:] {
		if g.total > best.total {
			best = g
		}
	}

	hw.ProductName = best.name
	hw.DriverVersion = best.driver
	hw.CUDAVersion = cudaVersion
	hw.GPUCount = len(gpus)
	hw.MemoryKind = MemoryVRAM
	hw.UnifiedMemory = false
	hw.MemoryTotalBytes = best.total
	hw.MemoryUsedBytes = best.used
	hw.MemoryFreeBytes = best.free
	hw.UsableBytes = best.total

	if best.total > 0 && float64(best.free)/float64(best.total) < 0.50 {
		hw.Warnings = append(hw.Warnings,
			fmt.Sprintf("free VRAM (%.1f GiB) is much lower than total (%.1f GiB); models may not load until memory is freed",
				bytesToGiB(best.free), bytesToGiB(best.total)))
	}
	if len(gpus) > 1 {
		hw.Warnings = append(hw.Warnings,
			fmt.Sprintf("%d GPUs detected; scoring against largest single GPU (%.1f GiB)", len(gpus), bytesToGiB(best.total)))
	}
	return nil
}

func queryCUDAVersion() string {
	cmd := exec.Command("nvidia-smi")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "CUDA Version:") {
			idx := strings.Index(line, "CUDA Version:")
			rest := strings.TrimSpace(line[idx+len("CUDA Version:"):])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}

func splitCSV(line string) []string {
	var parts []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range line {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case r == ',' && !inQuotes:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

func bytesToGiB(b int64) float64 {
	return float64(b) / (1024 * 1024 * 1024)
}
