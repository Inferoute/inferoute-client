package compat

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTableNumbersAndUsableGreen(t *testing.T) {
	report := Report{
		Hardware: Hardware{OS: "darwin", Arch: "arm64"},
		Models: []ModelResult{
			{Alias: "gguf/phi3.5", ServiceType: "ollama", Status: StatusRunsWell, MinSizeBytes: 2 << 30, RequiredBytes: 3 << 30, Reason: "needs ~3 GiB"},
			{Alias: "gguf/mistral:7b", ServiceType: "ollama", Status: StatusFits, MinSizeBytes: 4 << 30, RequiredBytes: 5 << 30, Reason: "needs ~5 GiB"},
			{Alias: "huge", ServiceType: "ollama", Status: StatusTooLarge, MinSizeBytes: 40 << 30, RequiredBytes: 50 << 30, Reason: "too big"},
		},
		Summary: Summary{RunsWell: 1, Fits: 1, TooLarge: 1, Total: 3},
	}
	var buf bytes.Buffer
	if err := WriteTable(&buf, report); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "REASON") || strings.Contains(got, "needs ~3 GiB") {
		t.Fatalf("reason should be omitted:\n%s", got)
	}
	if !strings.Contains(got, "#") || !strings.Contains(got, "1") || !strings.Contains(got, "2") {
		t.Fatalf("expected numbered rows:\n%s", got)
	}
	if !strings.Contains(got, "gguf/phi3.5") || !strings.Contains(got, "gguf/mistral:7b") {
		t.Fatalf("missing models:\n%s", got)
	}
	green := "\033[1;32m"
	reset := "\033[0m"
	if !strings.Contains(got, green) || !strings.Contains(got, reset) {
		t.Fatalf("usable rows should be green:\n%s", got)
	}
	// too_large row is present and not wrapped in green
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "huge") && strings.Contains(line, green) {
			t.Fatalf("too_large should not be green: %q", line)
		}
	}
}
