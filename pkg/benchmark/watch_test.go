package benchmark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
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

func TestMeasureWaitsForInflightToDrain(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	var inflight atomic.Int32
	inflight.Store(1)
	var gateClosed atomic.Bool
	applied := 0
	w := &Watcher{
		ConfigPath: filepath.Join(t.TempDir(), "config.yaml"),
		EngineURL:  srv.URL,
		Model:      "m",
		client:     srv.Client(),
		Gate:       func(closed bool) { gateClosed.Store(closed) },
		Inflight: func() int32 {
			// With the gate open, new requests would keep the count up.
			if !gateClosed.Load() {
				t.Error("waited for requests with the gate open")
			}
			return inflight.Load()
		},
		Apply: func(n int) { applied = n },
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.measure(context.Background(), "fp")
	}()

	time.Sleep(200 * time.Millisecond)
	if got := requests.Load(); got != 0 {
		t.Fatalf("engine requests = %d while inflight, want 0", got)
	}

	inflight.Store(0)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("measure did not resume after inflight drained")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("engine requests = %d, want 1 after drain", got)
	}
	if applied != 4 {
		t.Fatalf("applied concurrency = %d, want fallback 4", applied)
	}
}

func TestMeasureSkipsWhenInflightNeverDrains(t *testing.T) {
	old := drainWait
	drainWait = 80 * time.Millisecond
	t.Cleanup(func() { drainWait = old })

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	applied := false
	w := &Watcher{
		ConfigPath: filepath.Join(t.TempDir(), "config.yaml"),
		EngineURL:  srv.URL,
		Model:      "m",
		client:     srv.Client(),
		Inflight:   func() int32 { return 1 },
		Apply:      func(n int) { applied = true },
	}
	w.measure(context.Background(), "fp")
	if requests.Load() != 0 {
		t.Fatal("must not hit the engine while inference is still in flight")
	}
	if applied || w.applied {
		t.Fatal("must not apply or cache a run that never started")
	}
}
