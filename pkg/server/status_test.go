package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if !strings.Contains(rec.Body.String(), "/api/status") {
		t.Fatal("dashboard html should poll /api/status")
	}
}

func TestHandleStatusMasksAPIKey(t *testing.T) {
	s := newTestServer("http://unused", &fakeLLM{})
	s.config.Provider.APIKey = "sk-abcdefghijklmnop"
	s.config.Provider.ProviderType = "ollama"
	s.config.Provider.LLMURL = "http://localhost:11434"
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
	if snap.ProviderType != "ollama" {
		t.Fatalf("provider_type = %q", snap.ProviderType)
	}
	if strings.Contains(snap.ProviderAPIKey, "efgh") || snap.ProviderAPIKey == s.config.Provider.APIKey {
		t.Fatalf("api key not masked: %q", snap.ProviderAPIKey)
	}
	if snap.LastHealthUpdate != "Never" {
		t.Fatalf("last_health_update = %q", snap.LastHealthUpdate)
	}
}
