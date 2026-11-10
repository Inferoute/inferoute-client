package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
)

func TestIsFatalServerErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "graceful shutdown", err: http.ErrServerClosed, want: false},
		{name: "wrapped shutdown", err: fmt.Errorf("serve: %w", http.ErrServerClosed), want: false},
		{name: "tunnel start failure", err: errors.New("failed to start tunnel: cloudflared process died during startup"), want: true},
		{name: "listen failure", err: errors.New("listen tcp :8080: bind: address already in use"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFatalServerErr(tt.err); got != tt.want {
				t.Fatalf("isFatalServerErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type listLLM struct {
	llm.Client
	models []llm.Model
	err    error
}

func (l listLLM) ListModels(context.Context) (*llm.ListModelsResponse, error) {
	if l.err != nil {
		return nil, l.err
	}
	return &llm.ListModelsResponse{Models: l.models}, nil
}

// Turning on a tool parser changes the launch arguments, so it must change the
// fingerprint and trigger a new capability probe.
func TestDeploymentFingerprintTracksEngineArgs(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Model = "m"
	cfg.Provider.Engine = "vllm"
	cfg.Provider.EngineArgs = []string{"--model", "m"}
	engine := listLLM{models: []llm.Model{{ID: "m", WeightFingerprint: "w", MaxModelLen: 8192}}}

	plain := deploymentFingerprint(context.Background(), cfg, engine, nil)
	if plain == "" || plain != deploymentFingerprint(context.Background(), cfg, engine, nil) {
		t.Fatalf("fingerprint is not stable: %q", plain)
	}
	cfg.Provider.EngineArgs = []string{"--model", "m", "--enable-auto-tool-choice", "--api-key", "secret"}
	withTools := deploymentFingerprint(context.Background(), cfg, engine, nil)
	if withTools == plain {
		t.Fatal("engine args did not change the fingerprint")
	}
	if strings.Contains(withTools, "secret") {
		t.Fatalf("fingerprint leaks an engine argument: %q", withTools)
	}
}

// An engine that does not answer has no fingerprint. A partial one would look
// like a different deployment and reset the reported capabilities.
func TestDeploymentFingerprintEmptyWhileEngineIsDown(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.Model = "m"
	if fp := deploymentFingerprint(context.Background(), cfg, listLLM{err: errors.New("connection refused")}, nil); fp != "" {
		t.Fatalf("fingerprint = %q", fp)
	}
}
