package setup

import (
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	quoteRotateEvery = 8 * time.Second
	swallowColor     = "\033[38;2;215;253;82m" // Inferoute lime
	ansiReset        = "\033[0m"
)

// swallowFrames is the Inferoute swallow in flight: forked tail (Y), wingbeat
// (^ ~ v), beak (>). It flaps while weaving so it reads as flying, not spinning.
// Every frame is the same display width so the status text stays put.
var swallowFrames = []string{
	`Y^>     `,
	`Y~>     `,
	` Yv>    `,
	` Y~>    `,
	`  Y^>   `,
	`  Y~>   `,
	`   Yv>  `,
	`   Y~>  `,
	`    Y^> `,
	`    Y~> `,
	`     Yv>`,
	`     Y~>`,
	`    Y^> `,
	`    Y~> `,
	`   Yv>  `,
	`   Y~>  `,
	`  Y^>   `,
	`  Y~>   `,
	` Yv>    `,
	` Y~>    `,
}

func paintSwallow(i int) string {
	return swallowColor + swallowFrames[i%len(swallowFrames)] + ansiReset
}

func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTTY(f)
}

// spinWhile prints msg while fn runs. On a TTY it animates in place and
// rotates a random AI quote underneath.
func spinWhile(out io.Writer, msg string, fn func() error) error {
	if out == nil {
		out = os.Stdout
	}
	if !writerIsTTY(out) {
		fmt.Fprintln(out, msg+"...")
		return fn()
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		qi := pickQuote(-1)
		quoteAt := time.Now()
		nLines := 0
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		render := func(frame int) {
			lines := formatSpinner(frame, msg, waitQuotes[qi], writerTermWidth(out))
			if nLines > 0 {
				clearLines(out, nLines)
			}
			writeLines(out, lines)
			nLines = len(lines)
		}
		render(0)
		for {
			select {
			case <-stop:
				clearLines(out, nLines)
				return
			case <-tick.C:
				i++
				if time.Since(quoteAt) >= quoteRotateEvery {
					qi = pickQuote(qi)
					quoteAt = time.Now()
				}
				render(i)
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

func writerTermWidth(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		if n := termWidth(f); n > 0 {
			return n
		}
	}
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n >= 20 {
			return n
		}
	}
	return 80
}

func formatSpinner(frame int, msg, quote string, width int) []string {
	if width < 20 {
		width = 20
	}
	prefix := swallowFrames[frame%len(swallowFrames)] + " "
	prefixW := utf8.RuneCountInString(prefix)
	msgWidth := width - prefixW
	if msgWidth < 16 {
		msgWidth = 16
	}
	msgLines := wrapText(msg, msgWidth)
	if len(msgLines) == 0 {
		msgLines = []string{""}
	}
	lines := []string{paintSwallow(frame) + " " + msgLines[0]}
	indent := strings.Repeat(" ", prefixW)
	for _, extra := range msgLines[1:] {
		lines = append(lines, indent+extra)
	}
	qWidth := width - 2
	if qWidth < 16 {
		qWidth = 16
	}
	for _, q := range wrapText(quote, qWidth) {
		lines = append(lines, "  "+q)
	}
	return lines
}

func writeLines(out io.Writer, lines []string) {
	for i, line := range lines {
		fmt.Fprint(out, line)
		if i < len(lines)-1 {
			fmt.Fprint(out, "\n")
		}
	}
}

func clearLines(out io.Writer, n int) {
	if n <= 0 {
		return
	}
	fmt.Fprint(out, "\r\033[K")
	for i := 1; i < n; i++ {
		fmt.Fprint(out, "\033[1A\r\033[K")
	}
}

func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var b strings.Builder
	n := 0
	flush := func() {
		if n == 0 {
			return
		}
		lines = append(lines, b.String())
		b.Reset()
		n = 0
	}
	for _, w := range words {
		for utf8.RuneCountInString(w) > width {
			flush()
			r := []rune(w)
			lines = append(lines, string(r[:width]))
			w = string(r[width:])
		}
		if w == "" {
			continue
		}
		wl := utf8.RuneCountInString(w)
		if n == 0 {
			b.WriteString(w)
			n = wl
			continue
		}
		if n+1+wl > width {
			flush()
			b.WriteString(w)
			n = wl
			continue
		}
		b.WriteByte(' ')
		b.WriteString(w)
		n += 1 + wl
	}
	flush()
	return lines
}
