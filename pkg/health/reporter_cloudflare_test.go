package health

import "testing"

func TestCloudflareReportInfo(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		running bool
		want    bool
	}{
		{name: "running with url", url: "https://example.trycloudflare.com", running: true, want: true},
		{name: "hostname before process", url: "https://example.trycloudflare.com", running: false, want: false},
		{name: "running without url", url: "", running: true, want: false},
		{name: "stopped leftover hostname", url: "https://example.trycloudflare.com", running: false, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cloudflareReportInfo(tt.url, tt.running)
			if tt.want {
				if got == nil || got["url"] != tt.url {
					t.Fatalf("cloudflareReportInfo(%q, %v) = %v, want url", tt.url, tt.running, got)
				}
				return
			}
			if got != nil {
				t.Fatalf("cloudflareReportInfo(%q, %v) = %v, want nil", tt.url, tt.running, got)
			}
		})
	}
}
