package engine

import (
	"runtime"
	"strings"
)

// Kind is a local inference engine the wizard can install and start.
type Kind string

const (
	KindVLLM      Kind = "vllm"
	KindVLLMMetal Kind = "vllm-metal"
	KindFreeToken Kind = "freetoken"
)

// Label is the human-readable engine name for prompts and spinners.
func Label(k Kind) string {
	switch k {
	case KindVLLMMetal:
		return "vLLM Metal"
	case KindFreeToken:
		return "FreeToken"
	case KindVLLM:
		return "vLLM"
	default:
		return "vLLM"
	}
}

// ParseKind accepts wizard / config values.
func ParseKind(s string) (Kind, bool) {
	switch Kind(strings.ToLower(strings.TrimSpace(s))) {
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
	return "vllm"
}

// CatalogType is the approved-builds service_type used for compatibility scoring.
func CatalogType(k Kind) string {
	return PlatformType(k)
}

// CompatKind is the engine used to filter the catalog on this OS when the
// operator asked for a service_type rather than a runtime.
func CompatKind(goos, providerType string) Kind {
	switch goos {
	case "windows":
		return KindFreeToken
	case "darwin":
		return KindVLLMMetal
	default:
		return KindVLLM
	}
}

// RuntimeKind maps a requested engine (including the service-type alias "vllm")
// onto the runtime this OS actually installs and filters.
// Explicit freetoken / vllm-metal stay as given.
func RuntimeKind(goos string, requested Kind) Kind {
	if requested == KindVLLM {
		return CompatKind(goos, string(KindVLLM))
	}
	return requested
}

// DefaultURL is the loopback URL the client should probe.
func DefaultURL(k Kind) string {
	switch k {
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
		vllm := Option{Kind: KindVLLM, Label: "vLLM"}
		if !hasNVIDIA {
			vllm.Unavailable = "needs an NVIDIA GPU (nvidia-smi not found)"
		}
		return []Option{vllm}
	case "darwin":
		metal := Option{Kind: KindVLLMMetal, Label: "vLLM Metal"}
		if goarch != "arm64" {
			metal.Unavailable = "requires Apple Silicon"
		}
		return []Option{metal}
	case "windows":
		ft := Option{Kind: KindFreeToken, Label: "FreeToken"}
		if !hasNVIDIA {
			ft.Unavailable = "needs an NVIDIA GPU (nvidia-smi not found)"
		}
		return []Option{ft}
	default:
		return []Option{{Kind: KindVLLM, Label: "vLLM"}}
	}
}

// HostOptions is OptionsFor(runtime.GOOS, runtime.GOARCH, HasNVIDIA()).
func HostOptions() []Option {
	return OptionsFor(runtime.GOOS, runtime.GOARCH, HasNVIDIA())
}
