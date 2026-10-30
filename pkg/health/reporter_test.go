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
