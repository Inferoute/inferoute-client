//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableVirtualTerminal() {
	stdout := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(stdout, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(stdout, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}

func hideConsole() {
	hwnd, err := getConsoleWindow()
	if err != nil || hwnd == 0 {
		return
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	showWindow := user32.NewProc("ShowWindow")
	_, _, _ = showWindow.Call(uintptr(hwnd), uintptr(windows.SW_HIDE))
}

func getConsoleWindow() (windows.HWND, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("GetConsoleWindow")
	r1, _, err := proc.Call()
	if r1 == 0 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return 0, err
		}
		return 0, nil
	}
	return windows.HWND(r1), nil
}

func showErrorDialog(msg string) {
	_, _ = windows.MessageBox(0, windows.StringToUTF16Ptr(msg), windows.StringToUTF16Ptr("Inferoute Client"), windows.MB_OK|windows.MB_ICONERROR)
}
