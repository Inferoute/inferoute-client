package verify

import "strings"

const (
	EngineVLLM      = "vllm"
	EngineVLLMMetal = "vllm-metal"
	EngineLlamaCpp  = "llama_cpp"
)

// DefaultEngines is used when the catalog row omits engines.
// Empty llama_cpp rows default to llama.cpp. Other rows default to vLLM + vLLM Metal.
func DefaultEngines(serviceType string) []string {
	if strings.EqualFold(strings.TrimSpace(serviceType), EngineLlamaCpp) {
		return []string{EngineLlamaCpp}
	}
	return []string{EngineVLLM, EngineVLLMMetal}
}

// SupportsEngine reports whether this catalog row can be served by engineKind
// (vllm, vllm-metal, llama_cpp).
func SupportsEngine(entry CatalogEntry, engineKind string) bool {
	want := strings.ToLower(strings.TrimSpace(engineKind))
	if want == "" {
		return false
	}
	engines := entry.Engines
	if len(engines) == 0 {
		engines = DefaultEngines(entry.ServiceType)
	}
	for _, raw := range engines {
		if strings.EqualFold(strings.TrimSpace(raw), want) {
			return true
		}
	}
	return false
}

// FilterByEngine keeps catalog rows that the given engine can serve.
func FilterByEngine(entries []CatalogEntry, engineKind string) []CatalogEntry {
	out := make([]CatalogEntry, 0, len(entries))
	for _, e := range entries {
		if SupportsEngine(e, engineKind) {
			out = append(out, e)
		}
	}
	return out
}
