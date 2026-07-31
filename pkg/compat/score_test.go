package compat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

func TestScoreModelVRAMBoundaries(t *testing.T) {
	hw := &Hardware{
		MemoryKind:  MemoryVRAM,
		UsableBytes: 24 * 1024 * 1024 * 1024, // 24 GiB
	}

	cases := []struct {
		name   string
		size   int64
		svc    string
		want   FitStatus
	}{
		{"runs_well", 8 * 1024 * 1024 * 1024, "ollama", StatusRunsWell},  // 8GiB * 1.25 = 10 < 12
		{"fits", 14 * 1024 * 1024 * 1024, "ollama", StatusFits},           // 14*1.25=17.5 < 18
		{"tight", 17 * 1024 * 1024 * 1024, "ollama", StatusTight},         // 17*1.25=21.25 < 22.8
		{"too_large", 22 * 1024 * 1024 * 1024, "ollama", StatusTooLarge},  // 22*1.25=27.5 > 24
		{"unknown_size", 0, "ollama", StatusUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScoreModel(hw, verify.CatalogEntry{
				Alias:        "test/" + tc.name,
				ServiceType:  tc.svc,
				MinSizeBytes: tc.size,
				IsActive:     true,
			})
			if got.Status != tc.want {
				t.Fatalf("status=%s want=%s reason=%s required=%d usable=%d",
					got.Status, tc.want, got.Reason, got.RequiredBytes, got.UsableBytes)
			}
		})
	}
}

func TestScoreModelUnifiedMemoryReason(t *testing.T) {
	hw := &Hardware{
		MemoryKind:    MemoryUnified,
		UnifiedMemory: true,
		UsableBytes:    16 * 1024 * 1024 * 1024,
	}
	// Force tight: required ~15.6 GiB on 16 GiB usable.
	got := ScoreModel(hw, verify.CatalogEntry{
		Alias:        "Qwen/Qwen2.5-7B-Instruct",
		ServiceType:  "ollama",
		MinSizeBytes: 12 * 1024 * 1024 * 1024,
		IsActive:     true,
	})
	if got.Status != StatusTight {
		t.Fatalf("status=%s want tight", got.Status)
	}
	if !strings.Contains(got.Reason, "Apple Silicon") {
		t.Fatalf("expected Apple Silicon note in reason: %s", got.Reason)
	}
}

func TestScoreModelSystemRAMSlowWarning(t *testing.T) {
	hw := &Hardware{
		MemoryKind:  MemorySystem,
		UsableBytes:  32 * 1024 * 1024 * 1024,
	}
	got := ScoreModel(hw, verify.CatalogEntry{
		Alias:        "small/model",
		ServiceType:  "ollama",
		MinSizeBytes: 4 * 1024 * 1024 * 1024,
		IsActive:     true,
	})
	if got.Status != StatusRunsWell {
		t.Fatalf("status=%s", got.Status)
	}
	if !strings.Contains(got.Reason, "slow") {
		t.Fatalf("expected slow warning: %s", got.Reason)
	}
}

func TestVLLMUsesHigherOverhead(t *testing.T) {
	hw := &Hardware{MemoryKind: MemoryVRAM, UsableBytes: 20 * 1024 * 1024 * 1024}
	size := int64(12 * 1024 * 1024 * 1024)
	ollama := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "ollama", MinSizeBytes: size})
	vllm := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "vllm", MinSizeBytes: size})
	if ollama.RequiredBytes >= vllm.RequiredBytes {
		t.Fatalf("vllm required=%d should exceed ollama=%d", vllm.RequiredBytes, ollama.RequiredBytes)
	}
}

func TestReportJSONStableShape(t *testing.T) {
	hw := &Hardware{
		OS: "darwin", Arch: "arm64", ProductName: "Apple M2",
		MemoryKind: MemoryUnified, UnifiedMemory: true,
		SystemRAMBytes: 16 << 30, UsableBytes: 10 << 30,
	}
	results := []ModelResult{
		{Alias: "a", ServiceType: "ollama", Status: StatusFits, MinSizeBytes: 1, RequiredBytes: 2},
		{Alias: "b", ServiceType: "ollama", Status: StatusTooLarge, MinSizeBytes: 9, RequiredBytes: 10},
	}
	report := BuildReport(hw, results, false)
	if len(report.Models) != 1 {
		t.Fatalf("expected too_large filtered out, got %d", len(report.Models))
	}
	if report.Summary.TooLarge != 1 || report.Summary.Total != 2 {
		t.Fatalf("summary=%+v", report.Summary)
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"hardware", "models", "summary"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing key %s in %s", key, string(raw))
		}
	}
	body := strings.ToLower(string(raw))
	for _, secret := range []string{"expected_digest", "weight_fingerprint", "manifest", "sha256"} {
		if strings.Contains(body, secret) {
			t.Fatalf("JSON must not contain %s", secret)
		}
	}
}

func TestParseSystemProfilerGPU(t *testing.T) {
	// Exercise splitCSV used by Linux path.
	parts := splitCSV(`NVIDIA GeForce RTX 4090, 550.54.15, 24564, 1024, 23540, GPU-uuid`)
	if len(parts) < 5 {
		t.Fatalf("parts=%v", parts)
	}
	if parts[0] != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("name=%q", parts[0])
	}
}

func TestLoadOfflineCatalogFilters(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/catalog.json"
	payload := `{
	  "object":"list",
	  "data":[
	    {"alias":"a","service_type":"ollama","display_name":"A","min_size_bytes":100,"is_active":true},
	    {"alias":"b","service_type":"vllm","display_name":"B","min_size_bytes":200,"is_active":true},
	    {"alias":"c","service_type":"ollama","display_name":"C","min_size_bytes":300,"is_active":false}
	  ]
	}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadOfflineCatalog(path, "ollama")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Alias != "a" {
		t.Fatalf("entries=%+v", entries)
	}
}
