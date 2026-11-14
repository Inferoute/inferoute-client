package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Bodies captured from real engines on 2026-11-13.
const (
	vllmLinuxVersion = `{"version":"0.33.0"}`
	vllmMetalVersion = `{"version":"0.28.0"}`
	// llama-server /props, trimmed: the real body also carries settings and the chat template.
	llamaProps = `{"default_generation_settings":{"n_ctx":8192},"total_slots":4,"model_alias":"baai/bge-m3",` +
		`"build_info":"b11770-39da130fc","is_sleeping":false}`
)

func engineServer(t *testing.T, path string, status int, body string, delay time.Duration) *VLLMClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewVLLMClient(srv.URL, 30*time.Second).(*VLLMClient)
}

func TestEngineVersion(t *testing.T) {
	cases := []struct {
		name   string
		llama  bool
		path   string
		status int
		body   string
		want   string
	}{
		{"vllm", false, "/version", 200, vllmLinuxVersion, "0.33.0"},
		{"vllm metal", false, "/version", 200, vllmMetalVersion, "0.28.0"},
		{"llama.cpp keeps the release tag", true, "/props", 200, llamaProps, "b11770"},
		{"llama.cpp unofficial build", true, "/props", 200, `{"build_info":"unknown"}`, ""},
		{"llama.cpp without build_info", true, "/props", 200, `{"total_slots":4}`, ""},
		{"vllm without the route", false, "/elsewhere", 200, vllmLinuxVersion, ""},
		{"server error", false, "/version", 500, vllmLinuxVersion, ""},
		{"not json", false, "/version", 200, "<html>proxy login</html>", ""},
		{"asked vllm, got llama.cpp", false, "/version", 200, llamaProps, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := engineServer(t, tc.path, tc.status, tc.body, 0)
			if got := c.EngineVersion(context.Background(), tc.llama); got != tc.want {
				t.Fatalf("EngineVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

// A hung engine must not hold up the health report for longer than the lookup's bound,
// even though the client's own timeout (30s here) is much longer.
func TestEngineVersionGivesUpOnSlowEngine(t *testing.T) {
	c := engineServer(t, "/version", 200, vllmLinuxVersion, time.Minute)
	start := time.Now()
	if got := c.EngineVersion(context.Background(), false); got != "" {
		t.Fatalf("EngineVersion = %q, want empty", got)
	}
	if took := time.Since(start); took > engineVersionTimeout+2*time.Second {
		t.Fatalf("lookup took %s, want about %s", took, engineVersionTimeout)
	}
}

func TestEngineVersionUnreachable(t *testing.T) {
	c := NewVLLMClient("http://127.0.0.1:1", time.Second).(*VLLMClient)
	if got := c.EngineVersion(context.Background(), false); got != "" {
		t.Fatalf("EngineVersion = %q, want empty", got)
	}
}
