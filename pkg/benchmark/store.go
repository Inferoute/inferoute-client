package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Result is the cached measurement for one deployment fingerprint.
type Result struct {
	Fingerprint string        `json:"fingerprint"`
	Concurrency int           `json:"concurrency"`
	MeasuredAt  time.Time     `json:"measured_at"`
	Levels      []LevelResult `json:"levels"`
	GPUBusy     bool          `json:"gpu_busy,omitempty"`
}

func pathFor(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "benchmark.json")
}

func forcePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "benchmark.force")
}

func load(configPath string) (Result, bool) {
	raw, err := os.ReadFile(pathFor(configPath))
	if err != nil {
		return Result{}, false
	}
	var got Result
	if json.Unmarshal(raw, &got) != nil || got.Concurrency < 1 {
		return Result{}, false
	}
	return got, true
}

func save(configPath string, res Result) error {
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pathFor(configPath), raw, 0o644)
}

// Clear removes the cached result and asks a running client to measure again.
func Clear(configPath string) error {
	_ = os.Remove(pathFor(configPath))
	return os.WriteFile(forcePath(configPath), []byte("1\n"), 0o644)
}

func consumeForce(configPath string) bool {
	if _, err := os.Stat(forcePath(configPath)); err != nil {
		return false
	}
	_ = os.Remove(forcePath(configPath))
	_ = os.Remove(pathFor(configPath))
	return true
}
