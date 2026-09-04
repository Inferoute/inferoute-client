package engine

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const probeTimeout = 3 * time.Second

// Healthy reports whether the engine at llmURL is answering.
func Healthy(ctx context.Context, kind Kind, llmURL string) bool {
	llmURL = strings.TrimRight(strings.TrimSpace(llmURL), "/")
	if llmURL == "" {
		llmURL = DefaultURL(kind)
	}
	paths := []string{"/v1/models"}
	if kind == KindOllama {
		paths = []string{"/api/tags"}
	} else {
		paths = append(paths, "/health")
	}

	client := &http.Client{Timeout: probeTimeout}
	for _, p := range paths {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, llmURL+p, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 500 {
			return true
		}
	}
	return false
}

// WaitHealthy polls until Healthy or ctx is done.
func WaitHealthy(ctx context.Context, kind Kind, llmURL string, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if Healthy(ctx, kind, llmURL) {
		return nil
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s at %s: %w", kind, llmURL, ctx.Err())
		case <-t.C:
			if Healthy(ctx, kind, llmURL) {
				return nil
			}
		}
	}
}
