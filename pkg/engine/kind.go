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
	KindLlamaCpp  Kind = "llama_cpp"
)

// Label is the human-readable engine name for prompts and spinners.
func Label(k Kind) string {
	switch k {
	case KindVLLMMetal:
		return "vLLM Metal"
	case KindLlamaCpp:
		return "llama.cpp"
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
	case KindLlamaCpp:
		return KindLlamaCpp, true
	default:
		return "", false
	}
}

// PlatformType is the Inferoute provider_type for this engine.
func PlatformType(k Kind) string {
	if k == KindLlamaCpp {
		return "llama_cpp"
	}
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
		return KindLlamaCpp
	case "darwin":
		if providerType == string(KindLlamaCpp) {
			return KindLlamaCpp
		}
		return KindVLLMMetal
	default:
		return KindVLLM
	}
}

// RuntimeKind maps a requested engine (including the service-type alias "vllm")
// onto the runtime this OS actually installs and filters.
// Explicit llama_cpp / vllm-metal stay as given.
func RuntimeKind(goos string, requested Kind) Kind {
	if requested == KindVLLM {
		return CompatKind(goos, string(KindVLLM))
	}
	return requested
}

// DefaultURL is the loopback URL the client should probe.
func DefaultURL(k Kind) string {
	switch k {
	case KindLlamaCpp:
		return "http://127.0.0.1:8081"
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

// OptionsFor returns engines for this OS/arch.
// Linux is vLLM only. llama.cpp is Windows (NVIDIA) and Apple Silicon.
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
		llama := Option{Kind: KindLlamaCpp, Label: "llama.cpp"}
		if goarch != "arm64" {
			llama.Unavailable = "requires Apple Silicon"
		}
		return []Option{metal, llama}
	case "windows":
		llama := Option{Kind: KindLlamaCpp, Label: "llama.cpp"}
		if !hasNVIDIA {
			llama.Unavailable = "needs an NVIDIA GPU (nvidia-smi not found)"
		}
		return []Option{llama}
	default:
		return []Option{{Kind: KindVLLM, Label: "vLLM"}}
	}
}

// HostOptions is OptionsFor(runtime.GOOS, runtime.GOARCH, HasNVIDIA()).
func HostOptions() []Option {
	return OptionsFor(runtime.GOOS, runtime.GOARCH, HasNVIDIA())
}
