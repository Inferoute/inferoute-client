package gpu

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// macStats is what macOS reports about the GPU without sudo.
type macStats struct {
	Utilization float64 // percent, from IOAccelerator PerformanceStatistics
	UsedBytes   int64   // GPU-allocated system memory
	TotalBytes  int64   // memory the GPU may use (see gpuMemoryLimit)
}

var (
	reDeviceUtil = regexp.MustCompile(`"Device Utilization %"\s*=\s*(\d+)`)
	reInUseMem   = regexp.MustCompile(`"In use system memory"\s*=\s*(\d+)`)
)

// parseIORegAccelerator reads utilization and in-use memory from
// `ioreg -r -d 1 -w 0 -c IOAccelerator`. Missing keys stay zero.
func parseIORegAccelerator(out string) (util float64, used int64) {
	if m := reDeviceUtil.FindStringSubmatch(out); m != nil {
		util, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reInUseMem.FindStringSubmatch(out); m != nil {
		used, _ = strconv.ParseInt(m[1], 10, 64)
	}
	return util, used
}

// gpuMemoryLimit approximates Metal's recommendedMaxWorkingSetSize on unified
// memory: an explicit iogpu.wired_limit_mb wins, otherwise about 2/3 of RAM
// up to 36 GiB and 3/4 above that.
func gpuMemoryLimit(ramBytes, wiredLimitMB int64) int64 {
	if wiredLimitMB > 0 {
		return wiredLimitMB << 20
	}
	if ramBytes <= 36<<30 {
		return ramBytes * 2 / 3
	}
	return ramBytes * 3 / 4
}

func sysctlInt(ctx context.Context, name string) int64 {
	out, err := exec.CommandContext(ctx, "sysctl", "-n", name).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return n
}

func queryMacStats(ctx context.Context) macStats {
	var s macStats
	if out, err := exec.CommandContext(ctx, "ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator").Output(); err == nil {
		s.Utilization, s.UsedBytes = parseIORegAccelerator(string(out))
	}
	s.TotalBytes = gpuMemoryLimit(sysctlInt(ctx, "hw.memsize"), sysctlInt(ctx, "iogpu.wired_limit_mb"))
	return s
}
