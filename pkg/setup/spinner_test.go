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

func TestWrapTextPreservesContent(t *testing.T) {
	for _, q := range waitQuotes {
		for _, width := range []int{20, 40, 80, 120} {
			lines := wrapText(q, width)
			if len(lines) == 0 {
				t.Fatalf("wrapText(%q, %d) empty", q, width)
			}
			for i, line := range lines {
				if n := utf8.RuneCountInString(line); n > width {
					t.Fatalf("width %d line %d is %d runes: %q", width, i, n, line)
				}
			}
			got := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
			want := strings.Join(strings.Fields(q), " ")
			if got != want {
				t.Fatalf("lost text at width %d\ngot:  %s\nwant: %s", width, got, want)
			}
		}
	}
}

func TestWrapTextShortUnchanged(t *testing.T) {
	short := `"x" — Y`
	got := wrapText(short, 100)
	if len(got) != 1 || got[0] != short {
		t.Fatalf("wrapText(short) = %#v", got)
	}
}

func TestWrapTextHardBreaksLongWord(t *testing.T) {
	got := wrapText("abcde", 3)
	if strings.Join(got, "|") != "abc|de" {
		t.Fatalf("got %#v", got)
	}
}

func TestFormatSpinnerKeepsFullQuote(t *testing.T) {
	q := `"The question of whether a computer can think is no more interesting than the question of whether a submarine can swim." — Edsger W. Dijkstra`
	lines := formatSpinner(0, "Waiting for the engine", q, 80)
	body := strings.Join(lines, " ")
	if !strings.Contains(body, "submarine can swim") {
		t.Fatalf("quote truncated: %q", body)
	}
	if strings.Contains(body, "…") {
		t.Fatalf("still using ellipsis truncation: %q", body)
	}
	for i, line := range lines {
		visual := line
		visual = strings.ReplaceAll(visual, swallowColor, "")
		visual = strings.ReplaceAll(visual, ansiReset, "")
		if n := utf8.RuneCountInString(visual); n > 80 {
			t.Errorf("line %d visual width %d: %q", i, n, visual)
		}
	}
}
