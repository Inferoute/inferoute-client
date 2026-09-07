package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
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

// PortOpen reports whether something is accepting TCP on llmURL's host:port.
// True during HuggingFace download / model load, before HTTP is up.
func PortOpen(ctx context.Context, llmURL string) bool {
	addr := listenAddr(llmURL)
	d := net.Dialer{Timeout: time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func listenAddr(llmURL string) string {
	u, err := url.Parse(strings.TrimSpace(llmURL))
	if err != nil || u.Host == "" {
		return "127.0.0.1:8000"
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
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
