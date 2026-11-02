package inferscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadDoesNotKeepPartialFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "inferscale")
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Content-Length", "100000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("truncated"))
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		_, _ = w.Write([]byte("full-binary"))
	}))
	t.Cleanup(srv.Close)

	if err := download(context.Background(), srv.URL, dest); err == nil {
		t.Fatal("short body should fail")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("partial dest kept: %v", err)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp kept: %v", err)
	}
	if err := download(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "full-binary" {
		t.Fatalf("retry = %q %v", got, err)
	}
}

func TestDownloadSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "inferscale")
	if err := os.WriteFile(dest, []byte("keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("should not download")
	}))
	t.Cleanup(srv.Close)
	if err := download(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "keep" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestDownloadRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "inferscale")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if err := download(context.Background(), srv.URL, dest); err == nil {
		t.Fatal("empty body should fail")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("empty dest kept: %v", err)
	}
}
