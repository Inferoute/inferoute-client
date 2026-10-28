package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
)

func TestHandleDashboard(t *testing.T) {
	s := newTestServer("http://unused", &fakeLLM{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.handleDashboard(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/api/status") {
		t.Fatal("dashboard html should poll /api/status")
	}
	if strings.Contains(body, "Errors:") {
		t.Fatal("dashboard still renders an Errors section")
	}
}

func TestHandleStatusMasksAPIKey(t *testing.T) {
	s := newTestServer("http://unused", &fakeLLM{})
	s.config.Provider.APIKey = "sk-abcdefghijklmnop"
	s.config.Provider.ProviderType = "vllm"
	s.config.Provider.LLMURL = "http://127.0.0.1:8000"
	s.healthReporter = health.NewReporter(s.config, nil, &fakeLLM{})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	s.handleStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var snap StatusSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.SessionStatus != "online" {
		t.Fatalf("session = %q", snap.SessionStatus)
	}
	if snap.ProviderType != "vllm" {
		t.Fatalf("provider_type = %q", snap.ProviderType)
	}
	if strings.Contains(snap.ProviderAPIKey, "efgh") || snap.ProviderAPIKey == s.config.Provider.APIKey {
		t.Fatalf("api key not masked: %q", snap.ProviderAPIKey)
	}
	if snap.LastHealthUpdate != "Never" {
		t.Fatalf("last_health_update = %q", snap.LastHealthUpdate)
	}
}

func TestRecentRequestsAreInferenceOnly(t *testing.T) {
	s := newTestServer("http://unused", &fakeLLM{})
	rec := httptest.NewRecorder()
	s.handleBusy(rec, httptest.NewRequest(http.MethodGet, "/api/busy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("busy status = %d", rec.Code)
	}
	if len(s.snapshot().RecentRequests) != 0 {
		t.Fatalf("busy request logged: %+v", s.snapshot().RecentRequests)
	}

	s.logRequest(http.MethodPost, "/v1/chat/completions", http.StatusOK, time.Now())
	st := httptest.NewRecorder()
	s.handleStatus(st, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if strings.Contains(st.Body.String(), `"errors"`) {
		t.Fatalf("status includes errors: %s", st.Body.String())
	}
	var snap StatusSnapshot
	if err := json.Unmarshal(st.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.RecentRequests) != 1 || snap.RecentRequests[0].Path != "/v1/chat/completions" {
		t.Fatalf("recent = %+v", snap.RecentRequests)
	}
}

func TestStopWithoutStartDoesNotPanic(t *testing.T) {
	s := &Server{}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop on unstarted server: %v", err)
	}
}

func TestStartReturnsListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := &config.Config{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	cfg.Server.RequestTimeoutSeconds = 5
	s := &Server{config: cfg}
	if err := s.Start(); err == nil {
		t.Fatal("expected listen error when port is already taken")
	}
}
