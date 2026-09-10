package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitUntilDashboardReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitUntilDashboard(ctx, srv.URL+"/", time.Millisecond, nil); err != nil {
		t.Fatalf("waitUntilDashboard() = %v, want nil", err)
	}
}

func TestWaitUntilDashboardComesUp(t *testing.T) {
	var open atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !open.Load() {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	go func() {
		time.Sleep(80 * time.Millisecond)
		open.Store(true)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitUntilDashboard(ctx, srv.URL+"/", 20*time.Millisecond, nil); err != nil {
		t.Fatalf("waitUntilDashboard() = %v, want nil after server became ready", err)
	}
}

func TestWaitUntilDashboardTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := waitUntilDashboard(ctx, srv.URL+"/", 10*time.Millisecond, nil)
	if err == nil {
		t.Fatal("waitUntilDashboard() = nil, want timeout error")
	}
}

func TestWaitUntilDashboardProcessDied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitUntilDashboard(ctx, "http://127.0.0.1:1/", 10*time.Millisecond, func() bool { return false })
	if err == nil {
		t.Fatal("waitUntilDashboard() = nil, want process-exited error")
	}
}

func TestDashboardReadyRejectsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	if dashboardReady(context.Background(), srv.URL+"/") {
		t.Fatal("dashboardReady() = true, want false for 503")
	}
}
