//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const trayChildEnv = "INFEROUTE_TRAY_CHILD"

const (
	createBreakawayFromJob = 0x01000000
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

func freeConsole() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	_, _, _ = kernel32.NewProc("FreeConsole").Call()
}

// spawnDetachedIfNeeded restarts this process detached from the console so
// closing PowerShell does not stop the client. Returns true if the parent
// should exit immediately.
func spawnDetachedIfNeeded() bool {
	if os.Getenv(trayChildEnv) == "1" {
		return false
	}
	hwnd, err := getConsoleWindow()
	if err != nil || hwnd == 0 {
		return false
	}

	exe, err := os.Executable()
	if err != nil {
		hideAndDetachConsole()
		return false
	}

	flags := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW)
	if startDetached(exe, flags|createBreakawayFromJob) || startDetached(exe, flags) {
		fmt.Fprintln(os.Stderr, "Inferoute Client is running in the notification area.")
		fmt.Fprintln(os.Stderr, "Right-click the Inferoute icon to open the dashboard or quit.")
		return true
	}

	hideAndDetachConsole()
	return false
}

func startDetached(exe string, flags uint32) bool {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), trayChildEnv+"=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: flags,
	}
	return cmd.Start() == nil
}

func hideAndDetachConsole() {
	hideConsole()
	freeConsole()
}
