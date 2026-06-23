package usermsg

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
)

func TestConsoleLLMUnreachable(t *testing.T) {
	err := fmt.Errorf("list models: %w", fmt.Errorf("send: %w", llm.ErrUnreachable))
	got := Console(err, "vllm")
	want := "Could not connect to vLLM — is it running?"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConsoleLLMHTTP(t *testing.T) {
	err := fmt.Errorf("list models: %w", llm.ErrHTTP)
	got := Console(err, "ollama")
	want := "Ollama returned an error"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConsoleUnknown(t *testing.T) {
	got := Console(errors.New("something else"), "vllm")
	want := "Could not reach vLLM"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
