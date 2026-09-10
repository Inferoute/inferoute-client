package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	dashboardWaitTimeout  = 90 * time.Second
	dashboardPollInterval = 200 * time.Millisecond
	dashboardProbeTimeout = time.Second
)

func dashboardReady(ctx context.Context, dashboardURL string) bool {
	client := &http.Client{Timeout: dashboardProbeTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardURL, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// waitUntilDashboard polls dashboardURL until it returns 2xx, alive reports
// false, or ctx is done. alive may be nil.
func waitUntilDashboard(ctx context.Context, dashboardURL string, interval time.Duration, alive func() bool) error {
	if interval <= 0 {
		interval = dashboardPollInterval
	}
	if alive != nil && !alive() {
		return fmt.Errorf("client process exited before the dashboard was ready")
	}
	if dashboardReady(ctx, dashboardURL) {
		return nil
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for dashboard at %s", dashboardURL)
		case <-t.C:
			if alive != nil && !alive() {
				return fmt.Errorf("client process exited before the dashboard was ready")
			}
			if dashboardReady(ctx, dashboardURL) {
				return nil
			}
		}
	}
}
