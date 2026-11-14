package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})
	os.Exit(m.Run())
}

type stubLLM struct {
	models *llm.ListModelsResponse
	err    error
}

func (s stubLLM) ListModels(context.Context) (*llm.ListModelsResponse, error) {
	return s.models, s.err
}

func (s stubLLM) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, errors.New("unused")
}

func (s stubLLM) ForwardRequest(context.Context, string, []byte) (*http.Response, error) {
	return nil, errors.New("unused")
}

// versionedLLM is an engine that also answers the version lookup.
type versionedLLM struct {
	stubLLM
	version    string
	askedLlama *bool
}

func (v versionedLLM) EngineVersion(_ context.Context, llamaServer bool) string {
	if v.askedLlama != nil {
		*v.askedLlama = llamaServer
	}
	return v.version
}

func TestHealthReportCarriesEngineAndVersion(t *testing.T) {
	models := &llm.ListModelsResponse{Models: []llm.Model{{ID: "Qwen/Qwen2.5-7B-Instruct"}}}
	cases := []struct {
		name      string
		engine    string
		client    llm.Client
		wantKind  string
		wantVer   string
		wantLlama bool
	}{
		{"vllm", "vllm", versionedLLM{stubLLM: stubLLM{models: models}, version: "0.33.0"}, "vllm", "0.33.0", false},
		{"vllm metal", "vllm-metal", versionedLLM{stubLLM: stubLLM{models: models}, version: "0.28.0"}, "vllm-metal", "0.28.0", false},
		{"llama.cpp asks /props", "llama_cpp", versionedLLM{stubLLM: stubLLM{models: models}, version: "b11770"}, "llama_cpp", "b11770", true},
		// The lookup failed: the report still goes out, with the engine but no version.
		{"version lookup failed", "vllm", versionedLLM{stubLLM: stubLLM{models: models}}, "vllm", "", false},
		// A client without the lookup (a test double, a future engine client).
		{"client cannot tell", "vllm", stubLLM{models: models}, "vllm", "", false},
		// No engine configured (pointed at an existing server): say nothing about it.
		{"no engine configured", "", versionedLLM{stubLLM: stubLLM{models: models}, version: "0.33.0"}, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked bool
			if v, ok := tc.client.(versionedLLM); ok {
				v.askedLlama = &asked
				tc.client = v
			}
			cfg := &config.Config{}
			cfg.Provider.Engine = tc.engine
			r := NewReporter(cfg, nil, tc.client)
			report, err := r.GetHealthReport(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if report.Engine != tc.wantKind || report.EngineVersion != tc.wantVer {
				t.Fatalf("engine = %q %q, want %q %q", report.Engine, report.EngineVersion, tc.wantKind, tc.wantVer)
			}
			if asked != tc.wantLlama {
				t.Fatalf("asked llama-server = %v, want %v", asked, tc.wantLlama)
			}
		})
	}
}

func TestWorkerHealthReportShowsMasterModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/provider/network/node/health" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer node-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{}
	cfg.Provider.URL = srv.URL
	cfg.Provider.ClusterMode = "worker"
	cfg.Provider.NodeToken = "node-token"
	cfg.Provider.Engine = "vllm"

	r := NewReporter(cfg, nil, stubLLM{
		models: &llm.ListModelsResponse{Models: []llm.Model{{ID: "Qwen/Qwen2.5-7B-Instruct"}}},
	})
	if err := r.SendHealthReport(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := r.GetDisplayedModels()
	if len(got) != 1 || got[0].ID != "Qwen/Qwen2.5-7B-Instruct" {
		t.Fatalf("models = %+v", got)
	}

	r.llmClient = stubLLM{err: errors.New("master down")}
	if err := r.SendHealthReport(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.GetDisplayedModels(); len(got) != 0 {
		t.Fatalf("models after master down = %+v", got)
	}
}
