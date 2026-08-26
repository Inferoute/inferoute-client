package usermsg

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
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

func TestApprovalLabel(t *testing.T) {
	if got := ApprovalLabel(string(verify.StatusVerified)); got != "approved" {
		t.Fatalf("got %q, want approved", got)
	}
	if got := ApprovalLabel(string(verify.StatusFailed)); got != "failed verification" {
		t.Fatalf("got %q, want failed verification", got)
	}
}
