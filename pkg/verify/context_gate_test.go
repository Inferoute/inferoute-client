package verify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
)

func TestApplyContextGate(t *testing.T) {
	t.Parallel()

	required := int64(131072)
	entry := CatalogEntry{Alias: "m", MaxModelLen: &required}

	t.Run("pass when live >= required", func(t *testing.T) {
		m := &llm.Model{ID: "m", VerificationStatus: string(StatusVerified), MaxModelLen: 131072}
		applyContextGate(context.Background(), nil, m, entry)
		if m.VerificationStatus != string(StatusVerified) {
			t.Fatalf("status=%s", m.VerificationStatus)
		}
	})

	t.Run("fail when live < required", func(t *testing.T) {
		m := &llm.Model{ID: "m", VerificationStatus: string(StatusVerified), MaxModelLen: 8192}
		applyContextGate(context.Background(), nil, m, entry)
		if m.VerificationStatus != string(StatusFailed) {
			t.Fatalf("status=%s", m.VerificationStatus)
		}
	})

	t.Run("skip when catalog null", func(t *testing.T) {
		m := &llm.Model{ID: "m", VerificationStatus: string(StatusVerified), MaxModelLen: 8192}
		applyContextGate(context.Background(), nil, m, CatalogEntry{Alias: "m"})
		if m.VerificationStatus != string(StatusVerified) {
			t.Fatalf("status=%s", m.VerificationStatus)
		}
	})

	t.Run("fail when unreadable", func(t *testing.T) {
		m := &llm.Model{ID: "m", VerificationStatus: string(StatusVerified)}
		applyContextGate(context.Background(), nil, m, entry)
		if m.VerificationStatus != string(StatusFailed) {
			t.Fatalf("status=%s", m.VerificationStatus)
		}
	})
}

func TestApplyContextGateFreeTokenCache(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/cache/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"geometry": map[string]any{"num_pages": 4, "page_size": 2048},
		})
	}))
	t.Cleanup(srv.Close)

	client := llm.NewVLLMClient(srv.URL, 0)
	required := int64(10000)
	m := &llm.Model{ID: "m", VerificationStatus: string(StatusVerified)}
	applyContextGate(context.Background(), client, m, CatalogEntry{Alias: "m", MaxModelLen: &required})
	if m.VerificationStatus != string(StatusFailed) {
		t.Fatalf("4*2048=8192 < 10000 should fail, got %s", m.VerificationStatus)
	}

	okReq := int64(8192)
	m2 := &llm.Model{ID: "m", VerificationStatus: string(StatusVerified)}
	applyContextGate(context.Background(), client, m2, CatalogEntry{Alias: "m", MaxModelLen: &okReq})
	if m2.VerificationStatus != string(StatusVerified) {
		t.Fatalf("exact match should pass, got %s", m2.VerificationStatus)
	}
}
