package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const probeTimeout = 3 * time.Second

// Healthy reports whether the engine at llmURL can serve inference.
//
// vLLM / vLLM Metal: /v1/models with at least one id. An empty 200 is not
// ready — vLLM lists models while weights are still downloading.
// llama.cpp: GET /health with status "ok". The server returns 503 while loading.
func Healthy(ctx context.Context, kind Kind, llmURL string) bool {
	llmURL = strings.TrimRight(strings.TrimSpace(llmURL), "/")
	if llmURL == "" {
		llmURL = DefaultURL(kind)
	}

	client := &http.Client{Timeout: probeTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llmURL+probePath(kind), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	return bodyReady(kind, body)
}

func probePath(kind Kind) string {
	switch kind {
	case KindLlamaCpp:
		return "/health"
	default:
		return "/v1/models"
	}
}

func bodyReady(kind Kind, body []byte) bool {
	if kind == KindLlamaCpp {
		return statusOK(body)
	}
	return hasLoadedModel(body)
}

func statusOK(body []byte) bool {
	var h struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &h) != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(h.Status), "ok")
}

func hasLoadedModel(body []byte) bool {
	var listing struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &listing) != nil {
		return false
	}
	return len(listing.Data) > 0
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

// WaitListen polls until addr accepts TCP, the engine process exits, or ctx is done.
// addr is host:port. exited may be nil.
func WaitListen(ctx context.Context, addr string, interval time.Duration, exited <-chan error) error {
	return waitUntil(ctx, "tcp://"+addr, interval, exited, func() bool {
		return PortOpen(ctx, "http://"+addr)
	})
}

// WaitHealthy polls until Healthy, the engine process exits, or ctx is done.
// exited may be nil.
func WaitHealthy(ctx context.Context, kind Kind, llmURL string, interval time.Duration, exited <-chan error) error {
	return waitUntil(ctx, llmURL, interval, exited, func() bool {
		return Healthy(ctx, kind, llmURL)
	})
}

func waitUntil(ctx context.Context, what string, interval time.Duration, exited <-chan error, ready func() bool) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if ready() {
		return nil
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case err := <-exited:
			if err != nil {
				return fmt.Errorf("process exited: %w", err)
			}
			return fmt.Errorf("process exited before %s was ready", what)
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s: %w", what, ctx.Err())
		case <-t.C:
			if ready() {
				return nil
			}
		}
	}
}
