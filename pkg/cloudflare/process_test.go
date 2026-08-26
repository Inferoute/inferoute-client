package cloudflare

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCloudflaredLogPath(t *testing.T) {
	p := cloudflaredLogPath()
	if filepath.Base(p) != "cloudflared-debug.log" {
		t.Fatalf("got %s", p)
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(p, "/tmp/") {
		t.Fatalf("windows must not use /tmp: %s", p)
	}
}
