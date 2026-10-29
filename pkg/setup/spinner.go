package setup

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unicode/utf8"
)

func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTTY(f)
}

// SpinWhile prints msg while fn runs. On a TTY it animates one line in place.
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

	width := 0
	if f, ok := out.(*os.File); ok {
		width = termWidth(f)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		i := 0
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		draw := func() {
			line := fitWidth(fmt.Sprintf("%c %s", frames[i%len(frames)], msg()), width)
			fmt.Fprintf(out, "\r\033[K%s", line)
		}
		draw()
		for {
			select {
			case <-stop:
				fmt.Fprint(out, "\r\033[K")
				return
			case <-tick.C:
				i++
				draw()
			}
		}
	}()

	err := fn()
	close(stop)
	wg.Wait()
	return err
}

// fitWidth keeps the spinner on one row. A wrapped line makes \r redraw only
// the last visual row, so the previous row stays and the spinner stacks.
func fitWidth(s string, width int) string {
	if width <= 1 {
		return s
	}
	max := width - 1
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}
