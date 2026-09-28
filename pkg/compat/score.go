package compat

import (
	"fmt"
	"strings"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

// FitStatus is a conservative model-fit classification.
type FitStatus string

const (
	StatusRunsWell FitStatus = "runs_well"
	StatusFits     FitStatus = "fits"
	StatusTight    FitStatus = "tight"
	StatusTooLarge FitStatus = "too_large"
	StatusUnknown  FitStatus = "unknown"
)

// ModelResult is one approved build scored against local hardware.
type ModelResult struct {
	Alias         string    `json:"alias"`
	DisplayName   string    `json:"display_name"`
	ServiceType   string    `json:"service_type"`
	MinSizeBytes  int64     `json:"min_size_bytes"`
	RequiredBytes int64     `json:"required_bytes"`
	UsableBytes   int64     `json:"usable_bytes"`
	Status        FitStatus `json:"status"`
	Reason        string    `json:"reason"`
	HFRepo        *string   `json:"hf_repo,omitempty"`
	HFRef         *string   `json:"hf_ref,omitempty"`
	MaxModelLen   *int64    `json:"max_model_len,omitempty"`
	// MoeStrategy is set for FreeToken only: engine.MoeFused when the model
	// fits in VRAM, engine.MoeAuto when experts must live in system RAM.
	MoeStrategy string `json:"moe_strategy,omitempty"`
}

// ScoreModels scores approved catalog entries for the engine that will serve them.
func ScoreModels(hw *Hardware, entries []verify.CatalogEntry, kind engine.Kind) []ModelResult {
	out := make([]ModelResult, 0, len(entries))
	for _, entry := range entries {
		out = append(out, ScoreModelFor(hw, entry, kind))
	}
	return out
}

// ScoreModelFor scores one entry for a specific engine. FreeToken can fall back
// to host-RAM expert offload when the GPU alone is too small; other engines use
// the plain memory-pool fit from ScoreModel.
func ScoreModelFor(hw *Hardware, entry verify.CatalogEntry, kind engine.Kind) ModelResult {
	res := ScoreModel(hw, entry)
	if kind != engine.KindFreeToken {
		return res
	}
	return applyFreeTokenStrategy(hw, entry, res)
}

// FreeToken's fused loader clones tensors while copying weights to the GPU, so
// it peaks well above the resident size. A `tight` VRAM fit has OOMed in
// practice, so only `runs_well` / `fits` are trusted for fused. Anything
// tighter is re-scored for offload: weights resident in system RAM, attention
// and KV on the GPU.
func applyFreeTokenStrategy(hw *Hardware, entry verify.CatalogEntry, res ModelResult) ModelResult {
	switch res.Status {
	case StatusRunsWell, StatusFits:
		res.MoeStrategy = engine.MoeFused
		return res
	case StatusUnknown:
		return res
	}

	if hw == nil || hw.MemoryKind != MemoryVRAM || hw.SystemRAMBytes <= 0 {
		res.Status = StatusTooLarge
		res.Reason += "; FreeToken needs an NVIDIA GPU with room for the whole model, or system RAM for expert offload"
		return res
	}

	hostRequired := int64(float64(entry.MinSizeBytes) * vllmContextRuntimeFactor)
	ratio := float64(hostRequired) / float64(hw.SystemRAMBytes)
	status := statusForRatio(ratio)
	if status == StatusTooLarge {
		res.Status = StatusTooLarge
		res.Reason = fmt.Sprintf("needs ~%s VRAM or ~%s system RAM for expert offload; usable %s (vram), %s system RAM",
			formatBytes(res.RequiredBytes), formatBytes(hostRequired), formatBytes(hw.UsableBytes), formatBytes(hw.SystemRAMBytes))
		return res
	}

	res.Status = status
	res.MoeStrategy = engine.MoeAuto
	res.RequiredBytes = hostRequired
	res.UsableBytes = hw.SystemRAMBytes
	res.Reason = fmt.Sprintf("MoE experts in system RAM (offload): needs ~%s host RAM, system RAM %s; attention + KV on the GPU (%s vram)",
		formatBytes(hostRequired), formatBytes(hw.SystemRAMBytes), formatBytes(hw.UsableBytes))
	if status == StatusTight {
		res.Reason += "; little headroom"
	}
	return res
}

func statusForRatio(ratio float64) FitStatus {
	switch {
	case ratio < 0.50:
		return StatusRunsWell
	case ratio < 0.75:
		return StatusFits
	case ratio < 0.95:
		return StatusTight
	default:
		return StatusTooLarge
	}
}

// Context-aware vLLM fit. When the catalog carries the model's exact KV cost
// (kv_cache_bytes_per_token = 2 * layers * kv_heads * head_dim * 2 for bf16),
// required = weights * 1.20 + kv_per_token * max_model_len.
// Without it we fall back to the ~3%-of-weights-per-8k heuristic.
const (
	baselineContextLen       int64 = 8192
	vllmContextRuntimeFactor       = 1.20 // weights + non-KV runtime
	vllmKVPerBaseline              = 0.03 // ~3% of weights per 8k tokens
)

// ScoreModel scores a single approved catalog entry.
func ScoreModel(hw *Hardware, entry verify.CatalogEntry) ModelResult {
	res := ModelResult{
		Alias:        entry.Alias,
		DisplayName:  entry.DisplayName,
		ServiceType:  entry.ServiceType,
		MinSizeBytes: entry.MinSizeBytes,
		UsableBytes:  0,
		HFRepo:       entry.HFRepo,
		HFRef:        entry.HFRef,
		MaxModelLen:  entry.MaxModelLen,
	}
	if res.DisplayName == "" {
		res.DisplayName = entry.Alias
	}
	if hw != nil {
		res.UsableBytes = hw.UsableBytes
	}

	if entry.MinSizeBytes <= 0 {
		res.Status = StatusUnknown
		res.Reason = "model size unavailable in catalog"
		return res
	}
	if hw == nil || hw.UsableBytes <= 0 {
		res.Status = StatusUnknown
		res.Reason = "usable memory unknown"
		return res
	}

	required := requiredMemoryBytes(entry.MinSizeBytes, entry.ServiceType, entry.MaxModelLen, entry.KVCacheBytesPerToken)
	res.RequiredBytes = required

	baseReason := fmt.Sprintf("needs ~%s; usable %s (%s)",
		formatBytes(required), formatBytes(hw.UsableBytes), hw.MemoryKind)
	if entry.MaxModelLen != nil && *entry.MaxModelLen > 0 && strings.EqualFold(entry.ServiceType, "vllm") {
		baseReason = fmt.Sprintf("needs ~%s (incl. %s context); usable %s (%s)",
			formatBytes(required), formatContextTokens(*entry.MaxModelLen), formatBytes(hw.UsableBytes), hw.MemoryKind)
	}

	res.Status = statusForRatio(float64(required) / float64(hw.UsableBytes))
	res.Reason = baseReason
	if res.Status == StatusTight {
		res.Reason += "; little headroom"
	}

	switch hw.MemoryKind {
	case MemorySystem:
		if res.Status == StatusRunsWell || res.Status == StatusFits || res.Status == StatusTight {
			res.Reason += "; CPU/system-RAM path — expect slow inference"
		}
	case MemoryUnified:
		if res.Status == StatusTight {
			res.Reason += "; Apple Silicon unified memory is shared with the OS"
		}
	case MemoryVRAM:
		if hw.MemoryFreeBytes > 0 && hw.MemoryFreeBytes < required && res.Status != StatusTooLarge {
			res.Reason += fmt.Sprintf("; free VRAM currently %s", formatBytes(hw.MemoryFreeBytes))
		}
	}

	return res
}

func requiredMemoryBytes(minSizeBytes int64, serviceType string, maxModelLen, kvBytesPerToken *int64) int64 {
	if maxModelLen == nil || *maxModelLen <= 0 || !strings.EqualFold(strings.TrimSpace(serviceType), "vllm") {
		return int64(float64(minSizeBytes) * overheadFactor(serviceType))
	}
	runtime := float64(minSizeBytes) * vllmContextRuntimeFactor
	if kvBytesPerToken != nil && *kvBytesPerToken > 0 {
		// Exact per-model KV cost from the catalog.
		return int64(runtime) + *kvBytesPerToken**maxModelLen
	}
	scale := float64(*maxModelLen) / float64(baselineContextLen)
	if scale < 1 {
		scale = 1
	}
	kv := float64(minSizeBytes) * vllmKVPerBaseline * scale
	return int64(runtime + kv)
}

func overheadFactor(serviceType string) float64 {
	switch strings.ToLower(strings.TrimSpace(serviceType)) {
	case "vllm":
		// KV cache + batching + CUDA graphs — conservative.
		return 1.50
	case "ollama":
		return 1.25
	default:
		return 1.35
	}
}

func formatContextTokens(n int64) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dk", n/1024)
	}
	return fmt.Sprintf("%d", n)
}

func formatBytes(b int64) string {
	if b < 0 {
		b = 0
	}
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)
	switch {
	case b >= tib:
		return fmt.Sprintf("%.2f TiB", float64(b)/float64(tib))
	case b >= gib:
		return fmt.Sprintf("%.2f GiB", float64(b)/float64(gib))
	case b >= mib:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(mib))
	case b >= kib:
		return fmt.Sprintf("%.0f KiB", float64(b)/float64(kib))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// StatusRank orders statuses for display (best first).
func StatusRank(s FitStatus) int {
	switch s {
	case StatusRunsWell:
		return 0
	case StatusFits:
		return 1
	case StatusTight:
		return 2
	case StatusUnknown:
		return 3
	case StatusTooLarge:
		return 4
	default:
		return 5
	}
}
