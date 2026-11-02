package inferscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadTruncatedRetries(t *testing.T) {
	payload := []byte("complete-binary")
	drop := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !drop {
			w.Write(payload)
			return
		}
		conn, bufrw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		bufrw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\nConnection: close\r\n\r\nshort")
		bufrw.Flush()
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "inferscaled")
	if err := download(context.Background(), srv.URL, dest); err == nil {
		t.Fatal("short body succeeded")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("dest left after short body: %v", err)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp left after short body: %v", err)
	}

	drop = false
	if err := download(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("retry = %q %v", got, err)
	}
	drop = true
	if err := download(context.Background(), srv.URL, dest); err != nil {
		t.Fatal(err)
	}
}
