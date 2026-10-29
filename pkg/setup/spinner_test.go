package setup

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSpinWhileNonTTY(t *testing.T) {
	var buf bytes.Buffer
	err := SpinWhile(&buf, "Fetching approved models from https://example", func() error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "Fetching approved models from https://example...") {
		t.Errorf("got %q", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Errorf("non-TTY spinner should print one line, got %q", got)
	}
}

func TestSpinWhilePropagatesError(t *testing.T) {
	want := errors.New("boom")
	err := SpinWhile(&bytes.Buffer{}, "x", func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestFitWidth(t *testing.T) {
	if got := fitWidth("hello", 0); got != "hello" {
		t.Fatalf("unknown width should not clip: %q", got)
	}
	if got := fitWidth("hello", 80); got != "hello" {
		t.Fatalf("short line should stay: %q", got)
	}
	got := fitWidth("Waiting for vLLM to start serving", 12)
	if utf8.RuneCountInString(got) != 11 {
		t.Fatalf("len = %d, want 11: %q", utf8.RuneCountInString(got), got)
	}
}
