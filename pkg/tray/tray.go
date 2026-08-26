//go:build windows

package tray

import (
	_ "embed"
	"os/exec"

	"fyne.io/systray"
)

//go:embed icon.ico
var icon []byte

// Options configure the Windows notification-area menu.
type Options struct {
	ConfigPath   string
	LogDir       string
	DashboardURL string
}

// Supported reports whether the system tray is available on this OS.
func Supported() bool { return true }

// Quit requests the tray event loop to exit.
func Quit() { systray.Quit() }

// Run blocks on the Windows tray event loop until Quit is called.
func Run(opts Options) {
	systray.Run(func() { onReady(opts) }, func() {})
}

func onReady(opts Options) {
	systray.SetIcon(icon)
	systray.SetTitle("Inferoute")
	systray.SetTooltip("Inferoute Provider Client")

	mDash := systray.AddMenuItem("Open dashboard", "Open Inferoute in the browser")
	mConfig := systray.AddMenuItem("Open config", "Edit config.yaml")
	mLogs := systray.AddMenuItem("Open logs", "Open the log folder")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop Inferoute Client")

	go func() {
		for {
			select {
			case <-mDash.ClickedCh:
				openURL(opts.DashboardURL)
			case <-mConfig.ClickedCh:
				openFile(opts.ConfigPath)
			case <-mLogs.ClickedCh:
				openDir(opts.LogDir)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func openURL(u string) {
	if u == "" {
		return
	}
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

func openFile(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("notepad.exe", path).Start()
}

func openDir(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("explorer.exe", path).Start()
}
