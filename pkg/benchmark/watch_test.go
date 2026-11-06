package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestMeasureStopsAfterFailedFirstLevel(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	applied := 0
	w := &Watcher{
		ConfigPath: filepath.Join(t.TempDir(), "config.yaml"),
		EngineURL:  srv.URL,
		Model:      "m",
		client:     srv.Client(),
		Apply:      func(n int) { applied = n },
	}
	w.measure(context.Background(), "fp")

	if got := requests.Load(); got != 1 {
		t.Fatalf("engine requests = %d, want 1 (must not climb 2/4/8/16 after n=1 failed)", got)
	}
	if applied != 4 {
		t.Fatalf("applied concurrency = %d, want fallback 4", applied)
	}
}
