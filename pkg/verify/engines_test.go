package verify

import "testing"

func TestSupportsEngineDefaults(t *testing.T) {
	vllm := CatalogEntry{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "vllm"}
	if !SupportsEngine(vllm, EngineVLLM) || !SupportsEngine(vllm, EngineVLLMMetal) {
		t.Fatal("untagged vllm row must support vllm and vllm-metal")
	}
	if SupportsEngine(vllm, EngineLlamaCpp) {
		t.Fatal("untagged vllm row must not imply llama_cpp")
	}

	unknown := CatalogEntry{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "custom"}
	if !SupportsEngine(unknown, EngineVLLM) || !SupportsEngine(unknown, EngineVLLMMetal) {
		t.Fatal("untagged unknown service type must use vLLM-family defaults")
	}
	if SupportsEngine(unknown, EngineLlamaCpp) {
		t.Fatal("untagged unknown service type must not imply llama_cpp")
	}
	llama := CatalogEntry{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "llama_cpp"}
	if !SupportsEngine(llama, EngineLlamaCpp) || SupportsEngine(llama, EngineVLLM) {
		t.Fatal("untagged llama_cpp row defaults to llama.cpp only")
	}
}

func TestFilterByEngineOptIn(t *testing.T) {
	entries := []CatalogEntry{
		{Alias: "baai/bge-m3", ServiceType: "vllm"},
		{Alias: "google/gemma-4-26B-A4B-it", ServiceType: "vllm", Engines: []string{"vllm", "vllm-metal", "llama_cpp"}},
		{Alias: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "vllm"},
	}
	got := FilterByEngine(entries, EngineLlamaCpp)
	if len(got) != 1 || got[0].Alias != "google/gemma-4-26B-A4B-it" {
		t.Fatalf("got %+v", got)
	}
}
