package setup

import (
	"fmt"
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

func TestEnrolledWorkerSkipsJoinPreview(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := &config.Config{}
	cfg.Provider.URL = srv.URL
	cfg.Provider.APIKey = "join-spent"
	cfg.Provider.ClusterMode = "worker"
	cfg.Provider.NodeToken = "node-abc"
	cfg.Provider.Model = "Qwen/Qwen2.5-Coder-32B-Instruct"
	cfg.Provider.Engine = "vllm"
	plan, err := clusterPlanReal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatal("preview was called")
	}
	if plan.Role != "worker" || plan.Model != cfg.Provider.Model {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestDropIfRemovedClearsToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.ClusterMode = "worker"
	cfg.Provider.NodeToken = "node-abc"
	err := dropIfRemoved(cfg, fmt.Errorf("POST /api/provider/network/node/engine: {\"message\":\"invalid node token\"}"))
	if err == nil || !strings.Contains(err.Error(), "owner removed") {
		t.Fatal(err)
	}
	if cfg.EnrolledWorker() {
		t.Fatal("still enrolled")
	}
	if dropIfRemoved(cfg, fmt.Errorf("connection refused")) == nil {
		t.Fatal("other errors must pass through")
	}
}
