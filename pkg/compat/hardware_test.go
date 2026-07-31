package compat

import (
	"strings"
	"testing"
)

func TestParseNvidiaCSVQuoted(t *testing.T) {
	parts := splitCSV(`"NVIDIA GeForce RTX 4090", 550.54.15, 24564, 1024, 23540`)
	if len(parts) < 5 {
		t.Fatalf("parts=%v", parts)
	}
	if strings.TrimSpace(parts[0]) != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("got %q", parts[0])
	}
}

func TestFormatBytes(t *testing.T) {
	if got := formatBytes(1536 * 1024 * 1024); !strings.Contains(got, "GiB") && !strings.Contains(got, "MiB") {
		t.Fatalf("unexpected %s", got)
	}
	if got := formatBytes(512); got != "512 B" {
		t.Fatalf("got %s", got)
	}
}

func TestOverheadFactor(t *testing.T) {
	if overheadFactor("vllm") <= overheadFactor("ollama") {
		t.Fatal("vllm overhead should exceed ollama")
	}
}
