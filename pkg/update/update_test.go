package update

import (
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
)

func TestNotice(t *testing.T) {
	prev := version.Version
	version.Version = "1.2.0"
	t.Cleanup(func() { version.Version = prev })

	msg, required := Notice(Release{Latest: "1.4.0", Min: "1.0.0"})
	if required || msg != "Update available: 1.2.0 -> 1.4.0. Run 'inferoute-client update' then restart." {
		t.Fatalf("available: required=%v msg=%q", required, msg)
	}

	msg, required = Notice(Release{Latest: "1.4.0", Min: "1.3.0"})
	if !required || msg != "Update required: 1.2.0 -> 1.4.0. Run 'inferoute-client update' then restart." {
		t.Fatalf("required: required=%v msg=%q", required, msg)
	}

	msg, required = Notice(Release{Latest: "1.2.0", Min: "1.0.0"})
	if required || msg != "" {
		t.Fatalf("current: required=%v msg=%q", required, msg)
	}
}

func TestAssetURL(t *testing.T) {
	got := assetURL(Release{Latest: "1.4.0", DownloadURL: "https://github.com/inferoute/inferoute-client/releases/latest"}, "inferoute-client-linux-amd64.zip")
	want := "https://github.com/inferoute/inferoute-client/releases/download/v1.4.0/inferoute-client-linux-amd64.zip"
	if got != want {
		t.Fatalf("got %s", got)
	}
	sums := checksumURL(Release{Latest: "1.4.0", DownloadURL: "https://github.com/inferoute/inferoute-client/releases/latest"})
	if sums != "https://github.com/inferoute/inferoute-client/releases/download/v1.4.0/checksums.txt" {
		t.Fatalf("sums %s", sums)
	}
}
