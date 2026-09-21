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

	ollama := CatalogEntry{Alias: "llama3.2", ServiceType: "ollama"}
	if !SupportsEngine(ollama, EngineOllama) {
		t.Fatal("untagged ollama row must support ollama")
	}
	if SupportsEngine(ollama, EngineVLLM) {
		t.Fatal("untagged ollama row must not support vllm")
	}
}

func TestFilterByEngineOptIn(t *testing.T) {
	entries := []CatalogEntry{
		{Alias: "baai/bge-m3", ServiceType: "vllm"},
		{Alias: "google/gemma-4-26B-A4B-it", ServiceType: "vllm", Engines: []string{"vllm", "vllm-metal", "freetoken"}},
		{Alias: "llama3.2", ServiceType: "ollama"},
	}
	got := FilterByEngine(entries, EngineFreeToken)
	if len(got) != 1 || got[0].Alias != "google/gemma-4-26B-A4B-it" {
		t.Fatalf("got %+v", got)
	}
}
