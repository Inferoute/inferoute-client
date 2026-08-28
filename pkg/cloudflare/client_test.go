package cloudflare

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

func TestStopTunnelDisarmsRestartWhenNotRunning(t *testing.T) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})

	c := NewClient("http://example", "token", "http://localhost:8080")
	if !c.shouldRestart {
		t.Fatal("NewClient should arm supervision")
	}
	if err := c.StopTunnel(); err != nil {
		t.Fatalf("StopTunnel on unstarted client: %v", err)
	}
	if c.shouldRestart {
		t.Fatal("StopTunnel must disarm shouldRestart even when running is false")
	}
}

func TestStartTunnelFailureDisarmsRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cloudflared is a shell script")
	}
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})

	dir := t.TempDir()
	bin := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := NewClient("http://example", "token", "http://localhost:8080")
	c.token = "tunnel-token"
	if err := c.StartTunnel(context.Background()); err == nil {
		t.Fatal("expected StartTunnel to fail when cloudflared exits immediately")
	}
	defer c.StopTunnel()

	c.mu.RLock()
	armed := c.shouldRestart
	running := c.running
	c.mu.RUnlock()
	if armed {
		t.Fatal("failed StartTunnel left shouldRestart set; supervision would orphan cloudflared")
	}
	if running {
		t.Fatal("failed StartTunnel set running")
	}

	time.Sleep(1500 * time.Millisecond)
	if c.IsRunning() {
		t.Fatal("supervision restarted cloudflared after StartTunnel failed")
	}
}
