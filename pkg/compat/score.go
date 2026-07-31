package compat

import (
	"fmt"
	"strings"

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
	Alias            string    `json:"alias"`
	DisplayName      string    `json:"display_name"`
	ServiceType      string    `json:"service_type"`
	MinSizeBytes     int64     `json:"min_size_bytes"`
	RequiredBytes    int64     `json:"required_bytes"`
	UsableBytes      int64     `json:"usable_bytes"`
	Status           FitStatus `json:"status"`
	Reason           string    `json:"reason"`
	HFRepo           *string   `json:"hf_repo,omitempty"`
	HFRef            *string   `json:"hf_ref,omitempty"`
}

// ScoreModels scores approved catalog entries against detected hardware.
func ScoreModels(hw *Hardware, entries []verify.CatalogEntry) []ModelResult {
	out := make([]ModelResult, 0, len(entries))
	for _, entry := range entries {
		out = append(out, ScoreModel(hw, entry))
	}
	return out
}

// ScoreModel scores a single approved catalog entry.
func ScoreModel(hw *Hardware, entry verify.CatalogEntry) ModelResult {
	res := ModelResult{
		Alias:         entry.Alias,
		DisplayName:   entry.DisplayName,
		ServiceType:   entry.ServiceType,
		MinSizeBytes:  entry.MinSizeBytes,
		UsableBytes:    0,
		HFRepo:        entry.HFRepo,
		HFRef:         entry.HFRef,
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

	required := requiredMemoryBytes(entry.MinSizeBytes, entry.ServiceType)
	res.RequiredBytes = required

	ratio := float64(required) / float64(hw.UsableBytes)
	baseReason := fmt.Sprintf("needs ~%s; usable %s (%s)",
		formatBytes(required), formatBytes(hw.UsableBytes), hw.MemoryKind)

	switch {
	case ratio < 0.50:
		res.Status = StatusRunsWell
		res.Reason = baseReason
	case ratio < 0.75:
		res.Status = StatusFits
		res.Reason = baseReason
	case ratio < 0.95:
		res.Status = StatusTight
		res.Reason = baseReason + "; little headroom"
	default:
		res.Status = StatusTooLarge
		res.Reason = baseReason
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

func requiredMemoryBytes(minSizeBytes int64, serviceType string) int64 {
	return int64(float64(minSizeBytes) * overheadFactor(serviceType))
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
