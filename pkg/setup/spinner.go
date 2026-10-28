package setup

import (
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"sync"
	"time"
	"unicode/utf8"
)

const quoteRotateEvery = 8 * time.Second

// maxQuoteRunes keeps a quote on one line. Heisenberg's line is 101.
const maxQuoteRunes = 102

func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTTY(f)
}

// SpinWhile prints msg while fn runs. On a TTY it animates in place and
// rotates a quote underneath.
func SpinWhile(out io.Writer, msg string, fn func() error) error {
	return spinWhile(out, func() string { return msg }, fn)
}

// SpinWhileLive is SpinWhile with a message that can change while fn runs.
func SpinWhileLive(out io.Writer, msg func() string, fn func() error) error {
	if msg == nil {
		msg = func() string { return "" }
	}
	return spinWhile(out, msg, fn)
}

func spinWhile(out io.Writer, msg func() string, fn func() error) error {
	if out == nil {
		out = os.Stdout
	}
	if !writerIsTTY(out) {
		fmt.Fprintln(out, msg()+"...")
		return fn()
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		i := 0
		qi := pickQuote(-1)
		quoteAt := time.Now()
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		fmt.Fprintf(out, "%c %s\n  %s", frames[0], msg(), fitQuote(waitQuotes[qi]))
		for {
			select {
			case <-stop:
				fmt.Fprint(out, "\r\033[K\033[1A\r\033[K")
				return
			case <-tick.C:
				i++
				if time.Since(quoteAt) >= quoteRotateEvery {
					qi = pickQuote(qi)
					quoteAt = time.Now()
				}
				fmt.Fprintf(out, "\033[1A\r\033[K%c %s\n\033[K  %s", frames[i%len(frames)], msg(), fitQuote(waitQuotes[qi]))
			}
		}
	}()

	err := fn()
	close(stop)
	wg.Wait()
	return err
}

func pickQuote(prev int) int {
	n := len(waitQuotes)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return 0
	}
	for {
		i := rand.IntN(n)
		if i != prev {
			return i
		}
	}
}

func fitQuote(s string) string {
	if utf8.RuneCountInString(s) <= maxQuoteRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxQuoteRunes-1]) + "…"
}
