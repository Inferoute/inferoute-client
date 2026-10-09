package update

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestFetchReleaseReadsNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client/version" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latest":"1.4.0","min_supported":"1.0.0","download_url":"https://example.test/dl"}`))
	}))
	defer srv.Close()

	rel, err := FetchRelease(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Latest != "1.4.0" || rel.Min != "1.0.0" || rel.DownloadURL != "https://example.test/dl" {
		t.Fatalf("release = %+v", rel)
	}
}

func TestFetchReleaseEmptyLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"latest":"","min_supported":"","download_url":""}`))
	}))
	defer srv.Close()
	if _, err := FetchRelease(context.Background(), srv.URL); err == nil {
		t.Fatal("empty latest should fail")
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
