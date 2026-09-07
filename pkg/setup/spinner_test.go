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
	err := spinWhile(&buf, "Fetching approved models from https://example", func() error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "Fetching approved models from https://example...") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "—") {
		t.Errorf("non-TTY spinner should not print quotes: %q", got)
	}
}

func TestSpinWhilePropagatesError(t *testing.T) {
	want := errors.New("boom")
	err := spinWhile(&bytes.Buffer{}, "x", func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitQuotes(t *testing.T) {
	if len(waitQuotes) != 29 {
		t.Fatalf("len(waitQuotes) = %d, want 29", len(waitQuotes))
	}
	for i, q := range waitQuotes {
		if q == "" {
			t.Errorf("quote %d is empty", i)
		}
	}
}

func TestPickQuoteSkipsPrevious(t *testing.T) {
	seen := map[int]bool{}
	prev := pickQuote(-1)
	seen[prev] = true
	for i := 0; i < 80; i++ {
		n := pickQuote(prev)
		if n == prev {
			t.Fatalf("pickQuote(%d) repeated", prev)
		}
		if n < 0 || n >= len(waitQuotes) {
			t.Fatalf("pickQuote out of range %d", n)
		}
		seen[n] = true
		prev = n
	}
	if len(seen) < 10 {
		t.Fatalf("only hit %d distinct quotes", len(seen))
	}
}

func TestSwallowFramesSameWidth(t *testing.T) {
	if len(swallowFrames) == 0 {
		t.Fatal("swallowFrames is empty")
	}
	want := utf8.RuneCountInString(swallowFrames[0])
	if want == 0 {
		t.Fatal("swallow frame width is 0")
	}
	for i, f := range swallowFrames {
		if n := utf8.RuneCountInString(f); n != want {
			t.Errorf("frame %d width %d, want %d (%q)", i, n, want, f)
		}
	}
}

func TestPaintSwallowWraps(t *testing.T) {
	a := paintSwallow(0)
	b := paintSwallow(len(swallowFrames))
	if a != b {
		t.Fatalf("paintSwallow does not wrap: %q vs %q", a, b)
	}
	if !strings.Contains(a, swallowFrames[0]) {
		t.Fatalf("paintSwallow missing frame: %q", a)
	}
	if !strings.HasPrefix(a, swallowColor) || !strings.HasSuffix(a, ansiReset) {
		t.Fatalf("paintSwallow missing color wrap: %q", a)
	}
}

func TestFitQuote(t *testing.T) {
	short := `"x" — Y`
	if got := fitQuote(short); got != short {
		t.Fatalf("fitQuote(short) = %q", got)
	}
	runes := make([]rune, 120)
	for i := range runes {
		runes[i] = 'a'
	}
	got := fitQuote(string(runes))
	if utf8.RuneCountInString(got) != 100 {
		t.Fatalf("len = %d, want 100", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got)
	}
}
