package gpu

import "testing"

// Trimmed from `ioreg -r -d 1 -w 0 -c IOAccelerator` on an M-series Mac.
const sampleIOReg = `+-o AGXAcceleratorG14X  <class AGXAcceleratorG14X, id 0x100000a1c, registered, matched, active, busy 0 (0 ms), retain 103>
    {
      "IOClass" = "AGXAcceleratorG14X"
      "PerformanceStatistics" = {"In use system memory (driver)"=0,"Alloc system memory"=24510283776,"Tiler Utilization %"=31,"recoveryCount"=0,"lastRecoveryTime"=0,"Renderer Utilization %"=37,"TiledSceneBytes"=1048576,"Device Utilization %"=42,"SplitSceneCount"=0,"Allocated PB Size"=1179648,"In use system memory"=6442450944}
      "model" = "Apple M3 Max"
    }
`

func TestParseIORegAccelerator(t *testing.T) {
	util, used := parseIORegAccelerator(sampleIOReg)
	if util != 42 {
		t.Errorf("util = %v, want 42", util)
	}
	if used != 6442450944 {
		t.Errorf("used = %d, want 6442450944 (not the driver-only key)", used)
	}

	util, used = parseIORegAccelerator("no accelerator here")
	if util != 0 || used != 0 {
		t.Errorf("missing keys: got %v, %d, want zeros", util, used)
	}
}

func TestGPUMemoryLimit(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		name      string
		ram, wire int64
		want      int64
	}{
		{"16GiB uses two thirds", 16 * gib, 0, 16 * gib * 2 / 3},
		{"36GiB uses two thirds", 36 * gib, 0, 24 * gib},
		{"64GiB uses three quarters", 64 * gib, 0, 48 * gib},
		{"wired limit wins", 64 * gib, 57344, 56 * gib},
		{"unknown RAM", 0, 0, 0},
	}
	for _, c := range cases {
		if got := gpuMemoryLimit(c.ram, c.wire); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
