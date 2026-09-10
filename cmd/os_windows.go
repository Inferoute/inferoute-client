//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/setup"
	"golang.org/x/sys/windows"
)

const trayChildEnv = "INFEROUTE_TRAY_CHILD"

func isTrayChild() bool {
	return os.Getenv(trayChildEnv) == "1"
}

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
// should exit (child was spawned). The parent waits until the dashboard URL
// responds so the prompt does not return while the child is still starting.
func spawnDetachedIfNeeded(dashboardURL string) bool {
	if isTrayChild() {
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
	cmd := startDetached(exe, flags|createBreakawayFromJob)
	if cmd == nil {
		cmd = startDetached(exe, flags)
	}
	if cmd == nil {
		hideAndDetachConsole()
		return false
	}

	dead := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(dead)
	}()
	alive := func() bool {
		select {
		case <-dead:
			return false
		default:
			return true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), dashboardWaitTimeout)
	defer cancel()
	msg := fmt.Sprintf("Waiting for dashboard at %s", dashboardURL)
	if err := setup.SpinWhile(os.Stdout, msg, func() error {
		return waitUntilDashboard(ctx, dashboardURL, dashboardPollInterval, alive)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Inferoute Client failed to start: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "Inferoute Client is running in the notification area.")
	fmt.Fprintf(os.Stdout, "Dashboard: %s\n", dashboardURL)
	fmt.Fprintln(os.Stdout, "Right-click the Inferoute icon to open the dashboard or quit.")
	return true
}

func startDetached(exe string, flags uint32) *exec.Cmd {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), trayChildEnv+"=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: flags,
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	return cmd
}

func hideAndDetachConsole() {
	hideConsole()
	freeConsole()
}
