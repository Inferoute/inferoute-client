package setup

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTTY(f)
}

// SpinWhile prints msg while fn runs. On a TTY it stays on one line.
func SpinWhile(out io.Writer, msg string, fn func() error) error {
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
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		i := 0
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		draw := func() {
			fmt.Fprintf(out, "\r\033[K%c %s", frames[i%len(frames)], msg)
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
