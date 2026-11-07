package setup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
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

func TestEnrolledWorkerReadsPinFromEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/provider/network/node/engine" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"vllm_version":"0.31.0","master_gpu":"NVIDIA GeForce RTX 4090"}`))
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
	if plan.Role != "worker" || plan.Model != cfg.Provider.Model {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.VLLMVersion != "0.31.0" || plan.MasterGPU != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPrepareClusterSingleReplacesClusterCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/provider/network/session" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"cluster_mode":"single"}`))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Provider.URL = srv.URL
	cfg.Provider.APIKey = "sk-test"
	cfg.Provider.ClusterMode = "master"
	cfg.Provider.NodeToken = "node-old"
	cfg.Provider.EngineArgs = []string{"serve", "mistralai/Mistral-7B-Instruct-v0.3", "--nnodes", "2"}
	cfg.Provider.EngineEnv = []string{"NCCL_SOCKET_IFNAME=inferscale0"}
	spec := engine.ServeSpec(engine.KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "", engine.ServeOpts{})
	if err := prepareClusterReal(cfg, &spec); err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.ClusterMode != "" || cfg.Provider.NodeToken != "" {
		t.Fatalf("leftover mode=%q token=%q", cfg.Provider.ClusterMode, cfg.Provider.NodeToken)
	}
	if strings.Join(cfg.Provider.EngineArgs, " ") != strings.Join(spec.Args, " ") {
		t.Fatalf("args = %v", cfg.Provider.EngineArgs)
	}
	if len(cfg.Provider.EngineEnv) != 0 {
		t.Fatalf("env = %v", cfg.Provider.EngineEnv)
	}
}

func TestPrepareClusterLookupFailureKeepsClusterCommand(t *testing.T) {
	cfg := &config.Config{}
	cfg.Provider.URL = "http://127.0.0.1:1"
	cfg.Provider.APIKey = "sk-test"
	cfg.Provider.ClusterMode = "master"
	cfg.Provider.NodeToken = "node-old"
	cfg.Provider.EngineArgs = []string{"serve", "mistral", "--nnodes", "2"}
	spec := engine.ServeSpec(engine.KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "", engine.ServeOpts{})
	if err := prepareClusterReal(cfg, &spec); err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.ClusterMode != "master" || cfg.Provider.NodeToken != "node-old" {
		t.Fatalf("wiped cluster identity mode=%q token=%q", cfg.Provider.ClusterMode, cfg.Provider.NodeToken)
	}
	if strings.Join(cfg.Provider.EngineArgs, " ") != "serve mistral --nnodes 2" {
		t.Fatalf("args = %v", cfg.Provider.EngineArgs)
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
