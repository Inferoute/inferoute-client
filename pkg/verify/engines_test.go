package verify

import "testing"

func TestSupportsEngineDefaults(t *testing.T) {
	vllm := CatalogEntry{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "vllm"}
	if !SupportsEngine(vllm, EngineVLLM) || !SupportsEngine(vllm, EngineVLLMMetal) {
		t.Fatal("untagged vllm row must support vllm and vllm-metal")
	}
	if SupportsEngine(vllm, EngineFreeToken) {
		t.Fatal("untagged vllm row must not imply freetoken")
	}

	unknown := CatalogEntry{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "custom"}
	if !SupportsEngine(unknown, EngineVLLM) || !SupportsEngine(unknown, EngineVLLMMetal) {
		t.Fatal("untagged unknown service type must use vLLM-family defaults")
	}
	if SupportsEngine(unknown, EngineFreeToken) {
		t.Fatal("untagged unknown service type must not imply freetoken")
	}
}

func TestFilterByEngineOptIn(t *testing.T) {
	entries := []CatalogEntry{
		{Alias: "baai/bge-m3", ServiceType: "vllm"},
		{Alias: "google/gemma-4-26B-A4B-it", ServiceType: "vllm", Engines: []string{"vllm", "vllm-metal", "freetoken"}},
		{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "vllm"},
	}
	got := FilterByEngine(entries, EngineFreeToken)
	if len(got) != 1 || got[0].Alias != "google/gemma-4-26B-A4B-it" {
		t.Fatalf("got %+v", got)
	}
}
