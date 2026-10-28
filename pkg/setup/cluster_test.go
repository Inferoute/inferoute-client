package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
)

func TestCredentialKeepsJoinCode(t *testing.T) {
	if got := credential("Join code, shown once: join-abc."); got != "join-abc" {
		t.Fatalf("credential = %q", got)
	}
	if got := credential("sk-test"); got != "sk-test" {
		t.Fatalf("credential = %q", got)
	}
}

func TestWorkerWaitText(t *testing.T) {
	if got := workerWaitText(0, 1); got != "Waiting for workers to join (0/1)" {
		t.Fatalf("got %q", got)
	}
	if got := workerWaitText(1, 1); !strings.Contains(got, "1/1 workers joined") {
		t.Fatalf("got %q", got)
	}
}

func TestLoginServerFromEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/provider/network/engine" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"login_server":"http://127.0.0.1:8444","host":"198.18.0.1","port":8000}`))
	}))
	t.Cleanup(srv.Close)
	cfg := &config.Config{}
	cfg.Provider.URL = srv.URL
	cfg.Provider.APIKey = "key"
	if got := loginServer(cfg, false); got != "http://127.0.0.1:8444" {
		t.Fatalf("login server = %q", got)
	}
}

func TestLoginServerFallsBackToSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/provider/network/engine":
			http.Error(w, "master tailnet IP is not set", http.StatusConflict)
		case "/api/provider/network/session":
			_, _ = w.Write([]byte(`{"cluster_mode":"multi","login_server":"http://127.0.0.1:8444"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := &config.Config{}
	cfg.Provider.URL = srv.URL
	cfg.Provider.APIKey = "key"
	if got := loginServer(cfg, false); got != "http://127.0.0.1:8444" {
		t.Fatalf("login server = %q", got)
	}
}
