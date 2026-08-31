package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

func TestSupervisionLoopOutlivesStartupTimeout(t *testing.T) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})

	startup, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	c := &Client{
		restartCh:     make(chan struct{}, 1),
		shutdownCh:    make(chan struct{}),
		shouldRestart: true,
	}
	// Same binding StartTunnel uses: not a child of the startup timeout.
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.monitoringCtx, c.monitoringCancel = context.WithCancel(context.Background())
	defer c.cancel()
	defer c.monitoringCancel()

	exited := make(chan struct{})
	go func() {
		c.supervisionLoop()
		close(exited)
	}()

	select {
	case <-startup.Done():
	case <-time.After(time.Second):
		t.Fatal("startup timeout did not fire")
	}

	select {
	case <-exited:
		t.Fatal("supervision loop exited when the startup timeout fired")
	case <-time.After(80 * time.Millisecond):
	}

	c.monitoringCancel()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("supervision loop did not stop after monitoring cancel")
	}
}

func TestStartTunnelRejectsCanceledCallerContext(t *testing.T) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewClient("http://example", "token", "http://localhost:8080")
	c.token = "tunnel-token"
	if err := c.StartTunnel(ctx); err == nil {
		t.Fatal("expected StartTunnel to fail when caller context is already canceled")
	}
}

func TestRequestTunnelInvalidAPIKey(t *testing.T) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})

	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{
			name:   "401",
			status: http.StatusUnauthorized,
			body:   `{"error":"invalid API key"}`,
			want:   ErrInvalidAPIKey,
		},
		{
			name:   "401 empty body",
			status: http.StatusUnauthorized,
			body:   ``,
			want:   ErrInvalidAPIKey,
		},
		{
			name:   "consumer key",
			status: http.StatusUnauthorized,
			body:   `{"error":"API key is not associated with a provider"}`,
			want:   ErrInvalidAPIKey,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/cloudflare/tunnel/request" {
					t.Errorf("path = %q, want /api/cloudflare/tunnel/request", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer bad-key" {
					t.Errorf("Authorization = %q, want Bearer bad-key", got)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			c := NewClient(srv.URL, "bad-key", "http://localhost:8080")
			err := c.RequestTunnel(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("RequestTunnel() err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRequestTunnelSuccess(t *testing.T) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TunnelResponse{Token: "tok", Hostname: "host.example"})
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, "ok-key", "http://localhost:8080")
	if err := c.RequestTunnel(context.Background()); err != nil {
		t.Fatalf("RequestTunnel() err = %v, want nil", err)
	}
	if c.token != "tok" || c.hostname != "host.example" {
		t.Fatalf("token=%q hostname=%q, want tok / host.example", c.token, c.hostname)
	}
}

func TestTunnelRequestErrorOtherStatus(t *testing.T) {
	err := tunnelRequestError(http.StatusInternalServerError, []byte(`{"error":"Internal Server Error"}`))
	if errors.Is(err, ErrInvalidAPIKey) {
		t.Fatal("generic 500 must not map to ErrInvalidAPIKey")
	}
	if got := err.Error(); got != `tunnel API returned status 500: {"error":"Internal Server Error"}` {
		t.Fatalf("err = %q", got)
	}
}

func TestTunnelRequestErrorWrapped(t *testing.T) {
	err := fmt.Errorf("failed to request tunnel: %w", ErrInvalidAPIKey)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatal("wrapped ErrInvalidAPIKey not detected")
	}
}

