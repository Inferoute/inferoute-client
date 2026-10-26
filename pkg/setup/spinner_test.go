package setup

import (
	"bytes"
	"errors"
	"strings"
	"testing"
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
	if strings.Contains(got, "⠋") {
		t.Errorf("non-TTY spinner should not animate: %q", got)
	}
}

func TestSpinWhilePropagatesError(t *testing.T) {
	want := errors.New("boom")
	err := SpinWhile(&bytes.Buffer{}, "x", func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}
