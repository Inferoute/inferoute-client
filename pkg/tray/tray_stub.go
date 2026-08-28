//go:build !windows

package tray

// Options configure the Windows notification-area menu.
// The fields exist on all platforms so cmd/main.go can compile unchanged.
type Options struct {
	ConfigPath   string
	LogDir       string
	DashboardURL string
	Started      chan<- struct{}
}

// Supported reports whether the system tray is available on this OS.
func Supported() bool { return false }

// Quit is a no-op on non-Windows platforms.
func Quit() {}

// Run is a no-op on non-Windows platforms.
func Run(opts Options) {
	if opts.Started != nil {
		close(opts.Started)
	}
}
