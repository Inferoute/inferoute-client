package engine

import (
	"runtime"
	"strings"
)

// Kind is a local inference engine the wizard can install and start.
type Kind string

const (
	KindOllama    Kind = "ollama"
	KindVLLM      Kind = "vllm"
	KindVLLMMetal Kind = "vllm-metal"
	KindFreeToken Kind = "freetoken"
)

// ParseKind accepts wizard / config values.
func ParseKind(s string) (Kind, bool) {
	switch Kind(strings.ToLower(strings.TrimSpace(s))) {
	case KindOllama:
		return KindOllama, true
	case KindVLLM:
		return KindVLLM, true
	case KindVLLMMetal:
		return KindVLLMMetal, true
	case KindFreeToken:
		return KindFreeToken, true
	default:
		return "", false
	}
}

// PlatformType is the Inferoute provider_type for this engine.
func PlatformType(k Kind) string {
	if k == KindOllama {
		return "ollama"
	}
	return "vllm"
}

// CatalogType is the approved-builds service_type used for compatibility scoring.
func CatalogType(k Kind) string {
	return PlatformType(k)
}

// DefaultURL is the loopback URL the client should probe.
func DefaultURL(k Kind) string {
	switch k {
	case KindOllama:
		return "http://127.0.0.1:11434"
	case KindFreeToken:
		return "http://127.0.0.1:1919"
	default:
		return "http://127.0.0.1:8000"
	}
}

// Option is one engine the wizard may offer on this machine.
type Option struct {
	Kind        Kind
	Label       string
	Unavailable string // non-empty = show but not selectable
}

// OptionsFor returns engines for this OS/arch. NVIDIA is required for vLLM and FreeToken.
func OptionsFor(goos, goarch string, hasNVIDIA bool) []Option {
	switch goos {
	case "linux":
		opts := []Option{{Kind: KindOllama, Label: "Ollama"}}
		vllm := Option{Kind: KindVLLM, Label: "vLLM"}
		if !hasNVIDIA {
			vllm.Unavailable = "needs an NVIDIA GPU (nvidia-smi not found)"
		}
		return append(opts, vllm)
	case "darwin":
		opts := []Option{{Kind: KindOllama, Label: "Ollama"}}
		metal := Option{Kind: KindVLLMMetal, Label: "vLLM Metal"}
		if goarch != "arm64" {
			metal.Unavailable = "requires Apple Silicon"
		}
		return append(opts, metal)
	case "windows":
		opts := []Option{{Kind: KindOllama, Label: "Ollama"}}
		ft := Option{Kind: KindFreeToken, Label: "FreeToken"}
		if !hasNVIDIA {
			ft.Unavailable = "needs an NVIDIA GPU (nvidia-smi not found)"
		}
		return append(opts, ft)
	default:
		return []Option{{Kind: KindOllama, Label: "Ollama"}}
	}
}

// HostOptions is OptionsFor(runtime.GOOS, runtime.GOARCH, HasNVIDIA()).
func HostOptions() []Option {
	return OptionsFor(runtime.GOOS, runtime.GOARCH, HasNVIDIA())
}
