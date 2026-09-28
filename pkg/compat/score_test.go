package compat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

func TestScoreModelVRAMBoundaries(t *testing.T) {
	hw := &Hardware{
		MemoryKind:  MemoryVRAM,
		UsableBytes: 24 * 1024 * 1024 * 1024, // 24 GiB
	}

	cases := []struct {
		name string
		size int64
		svc  string
		want FitStatus
	}{
		{"runs_well", 8 * 1024 * 1024 * 1024, "ollama", StatusRunsWell},  // 8GiB * 1.25 = 10 < 12
		{"fits", 14 * 1024 * 1024 * 1024, "ollama", StatusFits},          // 14*1.25=17.5 < 18
		{"tight", 17 * 1024 * 1024 * 1024, "ollama", StatusTight},        // 17*1.25=21.25 < 22.8
		{"too_large", 22 * 1024 * 1024 * 1024, "ollama", StatusTooLarge}, // 22*1.25=27.5 > 24
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
		UsableBytes:   16 * 1024 * 1024 * 1024,
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
		UsableBytes: 32 * 1024 * 1024 * 1024,
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

func TestScoreModelContextKVScale(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	hw := &Hardware{MemoryKind: MemoryVRAM, UsableBytes: 24 * gib}
	size := int64(14 * gib) // ~Qwen 7B-class weights
	len8k := int64(8192)
	len32k := int64(32768)
	len128k := int64(131072)

	nullCtx := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "vllm", MinSizeBytes: size})
	ctx8 := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "vllm", MinSizeBytes: size, MaxModelLen: &len8k})
	ctx32 := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "vllm", MinSizeBytes: size, MaxModelLen: &len32k})
	ctx128 := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "vllm", MinSizeBytes: size, MaxModelLen: &len128k})

	if ctx32.RequiredBytes <= ctx8.RequiredBytes {
		t.Fatalf("32k required=%d should exceed 8k=%d", ctx32.RequiredBytes, ctx8.RequiredBytes)
	}
	if ctx128.RequiredBytes <= ctx32.RequiredBytes {
		t.Fatalf("128k required=%d should exceed 32k=%d", ctx128.RequiredBytes, ctx32.RequiredBytes)
	}
	// Flat 1.50 without catalog context stays conservative vs 8k/32k KV-aware.
	if ctx8.RequiredBytes >= nullCtx.RequiredBytes {
		t.Fatalf("8k KV-aware required=%d should be below flat 1.50=%d", ctx8.RequiredBytes, nullCtx.RequiredBytes)
	}
	if ctx128.Status != StatusTooLarge {
		t.Fatalf("14GiB @ 128k on 24GiB should be too_large, got %s required=%d", ctx128.Status, ctx128.RequiredBytes)
	}
	if !strings.Contains(ctx128.Reason, "context") {
		t.Fatalf("reason should mention context: %s", ctx128.Reason)
	}

	// 7B @ 128k must still fit a 96GB Mac (65% usable ≈ 62.4 GiB).
	mac := &Hardware{MemoryKind: MemoryUnified, UnifiedMemory: true, UsableBytes: 62 * gib}
	qwen7 := int64(14 * gib)
	mac128 := ScoreModel(mac, verify.CatalogEntry{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "vllm", MinSizeBytes: qwen7, MaxModelLen: &len128k})
	if mac128.Status == StatusTooLarge {
		t.Fatalf("7B @ 128k on 62GiB usable should fit, got %s required=%d", mac128.Status, mac128.RequiredBytes)
	}

	ollama := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "ollama", MinSizeBytes: size, MaxModelLen: &len128k})
	ollamaNull := ScoreModel(hw, verify.CatalogEntry{Alias: "m", ServiceType: "ollama", MinSizeBytes: size})
	if ollama.RequiredBytes != ollamaNull.RequiredBytes {
		t.Fatalf("ollama must ignore max_model_len: %d vs %d", ollama.RequiredBytes, ollamaNull.RequiredBytes)
	}
}

func TestScoreModelCatalogKVPerToken(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	len128k := int64(131072)
	size := int64(14 * gib) // Qwen 7B-class weights
	kvQwen7 := int64(57344) // 2 * 28 layers * 4 kv_heads * 128 head_dim * 2B

	// Exact: 14 GiB * 1.20 + 57344 * 131072 = 16.8 GiB + 7.0 GiB = 23.8 GiB.
	entry := verify.CatalogEntry{
		Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "vllm",
		MinSizeBytes: size, MaxModelLen: &len128k, KVCacheBytesPerToken: &kvQwen7,
	}
	wantRequired := int64(float64(size)*vllmContextRuntimeFactor) + kvQwen7*len128k

	mac := &Hardware{MemoryKind: MemoryUnified, UnifiedMemory: true, UsableBytes: 62 * gib}
	got := ScoreModel(mac, entry)
	if got.RequiredBytes != wantRequired {
		t.Fatalf("required=%d want=%d", got.RequiredBytes, wantRequired)
	}
	if got.Status == StatusTooLarge {
		t.Fatalf("7B with exact KV @128k on 62GiB should fit, got %s", got.Status)
	}

	// Catalog KV overrides the heuristic; both must scale with context but differ.
	heuristic := ScoreModel(mac, verify.CatalogEntry{
		Alias: "m", ServiceType: "vllm", MinSizeBytes: size, MaxModelLen: &len128k,
	})
	if heuristic.RequiredBytes == got.RequiredBytes {
		t.Fatalf("heuristic and exact KV should differ: both %d", got.RequiredBytes)
	}

	// 32B-class: 61 GiB weights + 32 GiB KV must be too_large on the Mac.
	kv32 := int64(262144)
	size32 := int64(61 * gib)
	got32 := ScoreModel(mac, verify.CatalogEntry{
		Alias: "Qwen/Qwen2.5-Coder-32B-Instruct", ServiceType: "vllm",
		MinSizeBytes: size32, MaxModelLen: &len128k, KVCacheBytesPerToken: &kv32,
	})
	if got32.Status != StatusTooLarge {
		t.Fatalf("32B @128k on 62GiB should be too_large, got %s required=%d", got32.Status, got32.RequiredBytes)
	}

	// Zero/negative KV values fall back to the heuristic.
	kvZero := int64(0)
	fallback := ScoreModel(mac, verify.CatalogEntry{
		Alias: "m", ServiceType: "vllm", MinSizeBytes: size, MaxModelLen: &len128k, KVCacheBytesPerToken: &kvZero,
	})
	if fallback.RequiredBytes != heuristic.RequiredBytes {
		t.Fatalf("kv=0 should fall back to heuristic: %d vs %d", fallback.RequiredBytes, heuristic.RequiredBytes)
	}

	// Non-vLLM ignores KV per token entirely.
	ollama := ScoreModel(mac, verify.CatalogEntry{
		Alias: "m", ServiceType: "ollama", MinSizeBytes: size, MaxModelLen: &len128k, KVCacheBytesPerToken: &kvQwen7,
	})
	ollamaNull := ScoreModel(mac, verify.CatalogEntry{Alias: "m", ServiceType: "ollama", MinSizeBytes: size})
	if ollama.RequiredBytes != ollamaNull.RequiredBytes {
		t.Fatalf("ollama must ignore kv_cache_bytes_per_token: %d vs %d", ollama.RequiredBytes, ollamaNull.RequiredBytes)
	}
}

func TestScoreModelForFreeTokenStrategy(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	len128k := int64(131072)
	kvGptOss := int64(24576)
	// openai/gpt-oss-20b: 12.84 GiB MXFP4 root checkpoint.
	gptOss := verify.CatalogEntry{
		Alias: "openai/gpt-oss-20b", ServiceType: "vllm", MinSizeBytes: 13789191359,
		MaxModelLen: &len128k, KVCacheBytesPerToken: &kvGptOss,
		Engines: []string{"vllm", "vllm-metal", "freetoken"},
	}
	l4 := &Hardware{MemoryKind: MemoryVRAM, UsableBytes: 23034 * 1024 * 1024, SystemRAMBytes: 32 * gib}

	// 18.4 GiB of 22.5 GiB is `tight` on VRAM. The fused loader OOMed there,
	// so FreeToken must fall back to host offload on a 32 GB box.
	got := ScoreModelFor(l4, gptOss, engine.KindFreeToken)
	if got.MoeStrategy != engine.MoeAuto {
		t.Fatalf("gpt-oss on L4 + 32 GiB RAM should offload, got strategy=%q status=%s reason=%s", got.MoeStrategy, got.Status, got.Reason)
	}
	if got.Status == StatusTooLarge || got.Status == StatusTight {
		t.Fatalf("offload on 32 GiB RAM should be a comfortable fit, got %s", got.Status)
	}
	if !strings.Contains(got.Reason, "offload") {
		t.Fatalf("reason should explain offload: %s", got.Reason)
	}

	// Same card, 16 GiB RAM: 15.4 GiB host requirement is 96% — too_large.
	small := &Hardware{MemoryKind: MemoryVRAM, UsableBytes: l4.UsableBytes, SystemRAMBytes: 16 * gib}
	if got := ScoreModelFor(small, gptOss, engine.KindFreeToken); got.Status != StatusTooLarge || got.MoeStrategy != "" {
		t.Fatalf("16 GiB RAM should be too_large, got %s strategy=%q", got.Status, got.MoeStrategy)
	}

	// 48 GiB card holds it outright — experts stay on the GPU.
	big := &Hardware{MemoryKind: MemoryVRAM, UsableBytes: 48 * gib, SystemRAMBytes: 32 * gib}
	if got := ScoreModelFor(big, gptOss, engine.KindFreeToken); got.MoeStrategy != engine.MoeFused || got.Status != StatusRunsWell {
		t.Fatalf("48 GiB VRAM should be fused/runs_well, got %s/%q", got.Status, got.MoeStrategy)
	}

	// vLLM on the same L4 keeps the plain VRAM verdict and no strategy.
	if got := ScoreModelFor(l4, gptOss, engine.KindVLLM); got.MoeStrategy != "" || got.Status != StatusTight {
		t.Fatalf("vllm must not get a MoE strategy, got %s/%q", got.Status, got.MoeStrategy)
	}

	// A 67 GiB BF16 MoE cannot offload into 32 GiB of RAM either.
	qwen35 := verify.CatalogEntry{Alias: "qwen/qwen3.6-35b-a3b", ServiceType: "vllm", MinSizeBytes: 71926681382, MaxModelLen: &len128k}
	if got := ScoreModelFor(l4, qwen35, engine.KindFreeToken); got.Status != StatusTooLarge {
		t.Fatalf("67 GiB weights should be too_large on 22 GiB VRAM + 32 GiB RAM, got %s", got.Status)
	}

	// No NVIDIA GPU: FreeToken cannot run at all.
	cpuOnly := &Hardware{MemoryKind: MemorySystem, UsableBytes: 22 * gib, SystemRAMBytes: 32 * gib}
	if got := ScoreModelFor(cpuOnly, gptOss, engine.KindFreeToken); got.Status != StatusTooLarge {
		t.Fatalf("system-RAM host should be too_large for FreeToken, got %s", got.Status)
	}

	// Unknown size stays unknown and unlabelled.
	if got := ScoreModelFor(l4, verify.CatalogEntry{Alias: "x", ServiceType: "vllm"}, engine.KindFreeToken); got.Status != StatusUnknown || got.MoeStrategy != "" {
		t.Fatalf("unknown size should stay unknown, got %s/%q", got.Status, got.MoeStrategy)
	}
}

func TestScoreEntriesForHostResolvesEngine(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	hw := &Hardware{MemoryKind: MemoryVRAM, UsableBytes: 22 * gib, SystemRAMBytes: 32 * gib}
	entries := []verify.CatalogEntry{
		{Alias: "gguf/x", ServiceType: "ollama", MinSizeBytes: 4 * gib},
		{Alias: "big/moe", ServiceType: "vllm", MinSizeBytes: 16 * gib, Engines: []string{"vllm", "freetoken"}},
	}
	win := scoreEntriesForHost(hw, entries, "", "windows")
	if win[0].MoeStrategy != "" {
		t.Fatalf("ollama row must not carry a MoE strategy: %+v", win[0])
	}
	if win[1].MoeStrategy != engine.MoeAuto {
		t.Fatalf("windows vllm row scores as FreeToken and should offload: %+v", win[1])
	}
	linux := scoreEntriesForHost(hw, entries, "", "linux")
	if linux[1].MoeStrategy != "" || linux[1].Status != StatusTooLarge {
		t.Fatalf("linux vllm row keeps the VRAM verdict: %+v", linux[1])
	}
	explicit := scoreEntriesForHost(hw, entries[1:], "freetoken", "linux")
	if explicit[0].MoeStrategy != engine.MoeAuto {
		t.Fatalf("--engine freetoken on linux should still offload: %+v", explicit[0])
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
	    {"alias":"c","service_type":"ollama","display_name":"C","min_size_bytes":300,"is_active":false},
	    {"alias":"d","service_type":"vllm","display_name":"D","min_size_bytes":200,"is_active":true,"engines":["vllm","vllm-metal","freetoken"]}
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

	all, err := LoadOfflineCatalog(path, "")
	if err != nil {
		t.Fatal(err)
	}
	win := filterEntriesForHost(all, "", "windows")
	if len(win) != 2 {
		t.Fatalf("windows host should keep ollama + freetoken-tagged vllm, got %+v", win)
	}
	linux := filterEntriesForHost(all, "", "linux")
	if len(linux) != 3 {
		t.Fatalf("linux should keep ollama+vllm, got %+v", linux)
	}
	ft := filterEntriesForHost(all, "freetoken", "linux")
	if len(ft) != 1 || ft[0].Alias != "d" {
		t.Fatalf("freetoken-tagged rows, got %+v", ft)
	}
	winVLLM := filterEntriesForHost(all, "vllm", "windows")
	if len(winVLLM) != 1 || winVLLM[0].Alias != "d" {
		t.Fatalf("windows --engine vllm should remap to freetoken and hide untagged, got %+v", winVLLM)
	}
	linuxVLLM := filterEntriesForHost(all, "vllm", "linux")
	if len(linuxVLLM) != 2 {
		t.Fatalf("linux --engine vllm should keep untagged+tagged, got %+v", linuxVLLM)
	}
	darwinVLLM := filterEntriesForHost(all, "vllm", "darwin")
	if len(darwinVLLM) != 2 {
		t.Fatalf("darwin --engine vllm should remap to vllm-metal (untagged implied), got %+v", darwinVLLM)
	}
}
