package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
)

func serverForNode(url string) *Server {
	cfg := &config.Config{}
	cfg.Provider.URL = url
	cfg.Provider.APIKey = "test-key"
	return &Server{config: cfg}
}

func TestValidateHMAC(t *testing.T) {
	t.Run("valid response passes", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("Authorization = %q, want Bearer test-key", got)
			}
			w.Write([]byte(`{"valid":true}`))
		}))
		defer ts.Close()

		if err := serverForNode(ts.URL).validateHMAC(context.Background(), "h"); err != nil {
			t.Fatalf("expected valid HMAC, got %v", err)
		}
	})

	t.Run("valid=false is rejected", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"valid":false}`))
		}))
		defer ts.Close()

		if err := serverForNode(ts.URL).validateHMAC(context.Background(), "h"); err == nil {
			t.Fatal("expected rejection when valid=false")
		}
	})

	t.Run("non-200 status is an error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer ts.Close()

		if err := serverForNode(ts.URL).validateHMAC(context.Background(), "h"); err == nil {
			t.Fatal("expected error on non-200 status")
		}
	})

	t.Run("malformed JSON is an error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`not json`))
		}))
		defer ts.Close()

		if err := serverForNode(ts.URL).validateHMAC(context.Background(), "h"); err == nil {
			t.Fatal("expected error on malformed JSON")
		}
	})
}
