package version

import (
	"fmt"
	"runtime"
)

// Build identity. Release builds set these with -ldflags -X.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// UserAgent identifies this binary to the Inferoute node.
func UserAgent() string {
	return fmt.Sprintf("inferoute-client/%s (%s; %s)", Version, runtime.GOOS, runtime.GOARCH)
}
