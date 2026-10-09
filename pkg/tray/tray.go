//go:build windows

package tray

import (
	_ "embed"
	"os/exec"

	"time"

	"fyne.io/systray"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
)

//go:embed icon.ico
var icon []byte

// Options configure the Windows notification-area menu.
type Options struct {
	ConfigPath   string
	LogDir       string
	DashboardURL string
	// Started is closed once the tray event loop is running. Quit is a no-op
	// until then; callers that exit on Start() failure must wait for this.
	Started chan<- struct{}
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
	if opts.Started != nil {
		close(opts.Started)
	}
	systray.SetIcon(icon)
	systray.SetTitle("Inferoute")
	systray.SetTooltip("Inferoute Provider Client")

	mDash := systray.AddMenuItem("Open dashboard", "Open the local status page")
	mConfig := systray.AddMenuItem("Open config", "Edit config.yaml")
	mLogs := systray.AddMenuItem("Open logs", "Open the log folder")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop Inferoute Client")

	showStartupNotice()
	go watchForUpdate()

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

func watchForUpdate() {
	var last nodeapi.UpdateStatus
	for {
		snap := nodeapi.State()
		if snap.Status != last && snap.Status != nodeapi.UpdateCurrent {
			tip := "Inferoute Provider Client"
			body := ""
			switch snap.Status {
			case nodeapi.UpdateRequired:
				tip = "Inferoute: update required"
				body = "Update required: " + version.Version + " -> " + snap.Latest + ". Run inferoute-client update, then restart."
			case nodeapi.UpdateAvailable:
				tip = "Inferoute: update available"
				body = "Update available: " + version.Version + " -> " + snap.Latest + ". Run inferoute-client update, then restart."
			}
			systray.SetTooltip(tip)
			Notify(tip, body)
			last = snap.Status
		}
		time.Sleep(5 * time.Second)
	}
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
