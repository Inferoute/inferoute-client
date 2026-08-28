package cloudflare

import (
	"context"
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
