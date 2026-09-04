//go:build windows

package setup

import (
	"io"
	"os"
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
