package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
	"go.uber.org/zap"
)

// Reporter handles health reporting to the central system
type Reporter struct {
	config          *config.Config
	gpuMonitor      *gpu.Monitor
	llmClient       llm.Client
	verifier        *verify.Verifier
	client          *http.Client
	lastUpdateTime  time.Time
	lastUpdateMutex sync.Mutex

	displayedModelsMu sync.RWMutex
	displayedModels   []llm.Model

	benchmarkMu sync.Mutex
	benchmark   *BenchmarkSummary
}

const (
	ReportInterval = 3 * time.Minute
)

// HealthReport represents a health report to be sent to the central system
type HealthReport struct {
	Object       string            `json:"object"`
	Data         []llm.Model       `json:"data"`
	GPU          *gpu.GPUInfo      `json:"gpu"`
	ProviderType string            `json:"provider_type"`
	Benchmark    *BenchmarkSummary `json:"benchmark,omitempty"`
}

// BenchmarkSummary is the last concurrency measurement, stored by the node.
type BenchmarkSummary struct {
	Concurrency int       `json:"concurrency"`
	MeasuredAt  time.Time `json:"measured_at"`
	Levels      any       `json:"levels,omitempty"`
}

// SetVerifier attaches the model integrity verifier (optional).
func (r *Reporter) SetVerifier(v *verify.Verifier) {
	r.verifier = v
}

// NewReporter creates a new health reporter
func NewReporter(cfg *config.Config, gpuMonitor *gpu.Monitor, llmClient llm.Client) *Reporter {
	return &Reporter{
		config:     cfg,
		gpuMonitor: gpuMonitor,
		llmClient:  llmClient,
		client:     nodeapi.NewHTTPClient(10 * time.Second),
	}
}

// SetBenchmark attaches the last concurrency measurement to health reports.
func (r *Reporter) SetBenchmark(b *BenchmarkSummary) {
	r.benchmarkMu.Lock()
	r.benchmark = b
	r.benchmarkMu.Unlock()
}

const maxAdvertisedModels = 5

// advertiseModels keeps approved catalog models, at most 5. The server rejects a larger push.
func (r *Reporter) advertiseModels(models []llm.Model) []llm.Model {
	if r.verifier == nil {
		return models
	}
	cat := r.verifier.CatalogClient()
	kept := make([]llm.Model, 0, len(models))
	for _, model := range models {
		if _, ok := cat.Get(model.ID); !ok {
			if _, ok = cat.Get(strings.TrimSuffix(model.ID, ":latest")); !ok {
				logger.Info("not an approved model, not advertised", zap.String("model", model.ID))
				continue
			}
		}
		kept = append(kept, model)
	}
	if len(kept) > maxAdvertisedModels {
		logger.Warn("serving more approved models than a health push allows; advertising the first 5",
			zap.Int("serving", len(kept)))
		kept = kept[:maxAdvertisedModels]
	}
	return kept
}

// SendHealthReport sends a health report to the central system
func (r *Reporter) SendHealthReport(ctx context.Context) error {
	if r.config.Provider.ClusterMode == "worker" {
		if models, err := r.llmClient.ListModels(ctx); err != nil || models == nil {
			r.setDisplayedModels(nil)
		} else {
			r.setDisplayedModels(models.Models)
		}
		status := "red"
		kind, _ := engine.ParseKind(r.config.Provider.Engine)
		if engine.WorkerAlive(kind, r.config.Provider.EngineArgs) {
			status = "green"
		}
		return r.sendNodeHealth(ctx, status, r.gpuInfo())
	}

	if r.verifier != nil {
		if err := r.verifier.RefreshCatalog(ctx); err != nil {
			logger.Warn("Failed to refresh approved model catalog", zap.Error(err))
		}
	}

	// Get health report
	report, err := r.GetHealthReport(ctx)
	if err != nil {
		return fmt.Errorf("failed to get health report: %w", err)
	}

	report.Data = r.advertiseModels(report.Data)

	logger.Info("Preparing to send health report", zap.Int("models", len(report.Data)))

	// Marshal report to JSON
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	// Log the full JSON payload (for debugging)
	logger.Debug("Health report JSON payload",
		zap.String("json_payload", string(reportJSON)))

	// Create request
	url := fmt.Sprintf("%s/api/provider/health", r.config.Provider.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reportJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.config.Provider.APIKey))

	logger.Info("Sending health report to provider",
		zap.String("url", url))

	// Send request
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Read and log response body for debugging
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr == nil {
			logger.Error("Health report error response",
				zap.String("response", string(respBody)))
			if up := nodeapi.ParseUpgrade(resp.StatusCode, respBody); up != nil {
				return up
			}
		}
		return fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	// Update last update time
	r.lastUpdateMutex.Lock()
	r.lastUpdateTime = time.Now()
	r.lastUpdateMutex.Unlock()

	logger.Info("Successfully sent health report")
	return nil
}

func (r *Reporter) sendNodeHealth(ctx context.Context, status string, gpuInfo *gpu.GPUInfo) error {
	token := r.config.Provider.NodeToken
	if token == "" {
		return fmt.Errorf("worker node token is not set")
	}
	body, _ := json.Marshal(struct {
		Status        string       `json:"status"`
		GPU           *gpu.GPUInfo `json:"gpu,omitempty"`
		EngineVersion string       `json:"engine_version,omitempty"`
	}{Status: status, GPU: gpuInfo, EngineVersion: installedEngineVersion(r)})
	url := fmt.Sprintf("%s/api/provider/network/node/health", r.config.Provider.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("node health: %s", bytes.TrimSpace(msg))
	}
	r.lastUpdateMutex.Lock()
	r.lastUpdateTime = time.Now()
	r.lastUpdateMutex.Unlock()
	return nil
}

func installedEngineVersion(r *Reporter) string {
	kind, ok := engine.ParseKind(r.config.Provider.Engine)
	if !ok || kind != engine.KindVLLM {
		return ""
	}
	return engine.InstalledVersion(engine.BinPath(kind, r.config.Provider.EngineBin))
}

// GetHealthReport gets the current health report
func (r *Reporter) GetHealthReport(ctx context.Context) (*HealthReport, error) {
	// Get list of models
	models, err := r.llmClient.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	enriched := r.enrichModels(ctx, models.Models)
	r.setDisplayedModels(enriched)

	gpuInfo := r.gpuInfo()

	r.benchmarkMu.Lock()
	bench := r.benchmark
	r.benchmarkMu.Unlock()

	report := &HealthReport{
		Object:       "list",
		Data:         enriched,
		GPU:          gpuInfo,
		ProviderType: r.config.Provider.ProviderType,
		Benchmark:    bench,
	}

	return report, nil
}

func (r *Reporter) gpuInfo() *gpu.GPUInfo {
	info := &gpu.GPUInfo{
		ProductName:   "Unknown",
		DriverVersion: "Unknown",
		CUDAVersion:   "Unknown",
	}
	if r.gpuMonitor != nil {
		got, err := r.gpuMonitor.GetGPUInfo()
		if err != nil {
			logger.Error("Failed to get GPU information", zap.Error(err))
		} else if got != nil {
			info = got
		}
	}
	return info
}

// GetLastUpdateTime gets the time of the last successful health update
func (r *Reporter) GetLastUpdateTime() time.Time {
	r.lastUpdateMutex.Lock()
	defer r.lastUpdateMutex.Unlock()
	return r.lastUpdateTime
}

// RefreshModelsForDisplay polls the local LLM and returns models with verification status.
// Used by the console UI to reflect local model changes between health reports.
func (r *Reporter) RefreshModelsForDisplay(ctx context.Context) ([]llm.Model, error) {
	models, err := r.llmClient.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	enriched := r.enrichModels(ctx, models.Models)
	r.setDisplayedModels(enriched)
	return enriched, nil
}

// GetDisplayedModels returns the last cached model status snapshot.
func (r *Reporter) GetDisplayedModels() []llm.Model {
	r.displayedModelsMu.RLock()
	defer r.displayedModelsMu.RUnlock()
	if len(r.displayedModels) == 0 {
		return nil
	}
	out := make([]llm.Model, len(r.displayedModels))
	copy(out, r.displayedModels)
	return out
}

func (r *Reporter) enrichModels(ctx context.Context, models []llm.Model) []llm.Model {
	if r.verifier == nil {
		return models
	}
	return r.verifier.ApplyToModels(ctx, r.llmClient, models)
}

func (r *Reporter) setDisplayedModels(models []llm.Model) {
	r.displayedModelsMu.Lock()
	r.displayedModels = models
	r.displayedModelsMu.Unlock()
}
