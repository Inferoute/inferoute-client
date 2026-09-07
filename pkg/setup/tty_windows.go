//go:build windows

package setup

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func inputReader() (io.Reader, io.Closer, error) {
	if isTTY(os.Stdin) {
		return os.Stdin, nil, nil
	}
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return os.Stdin, nil, nil
	}
	return f, f, nil
}

func termWidth(f *os.File) int {
	if f == nil {
		return 0
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(f.Fd()), &info); err != nil {
		return 0
	}
	w := int(info.Window.Right - info.Window.Left + 1)
	if w <= 0 {
		return 0
	}
	return w
}
