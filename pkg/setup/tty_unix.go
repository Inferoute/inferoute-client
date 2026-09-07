//go:build !windows

package setup

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func inputReader() (io.Reader, io.Closer, error) {
	if isTTY(os.Stdin) {
		return os.Stdin, nil, nil
	}
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return os.Stdin, nil, nil
	}
	return f, f, nil
}

func termWidth(f *os.File) int {
	if f == nil {
		return 0
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 {
		return 0
	}
	return int(ws.Col)
}
