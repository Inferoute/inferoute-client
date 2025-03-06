package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ollama"
	"go.uber.org/zap"
)

// Reporter handles health reporting to the central system
type Reporter struct {
	config          *config.Config
	gpuMonitor      *gpu.Monitor
	ollamaClient    *ollama.Client
	client          *http.Client
	lastUpdateTime  time.Time
	lastUpdateMutex sync.Mutex
}

// HealthReport represents a health report to be sent to the central system
type HealthReport struct {
	Object       string                 `json:"object"`
	Data         []ollama.OllamaModel   `json:"data"`
	GPU          *gpu.GPUInfo           `json:"gpu"`
	NGROK        map[string]interface{} `json:"ngrok"`
	ProviderType string                 `json:"provider_type"`
}

// NewReporter creates a new health reporter
func NewReporter(cfg *config.Config, gpuMonitor *gpu.Monitor, ollamaClient *ollama.Client) *Reporter {
	return &Reporter{
		config:       cfg,
		gpuMonitor:   gpuMonitor,
		ollamaClient: ollamaClient,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// SendHealthReport sends a health report to the central system
func (r *Reporter) SendHealthReport(ctx context.Context) error {
	logger.Debug("Preparing health report")

	// Get GPU information if available
	var gpuInfo *gpu.GPUInfo
	var err error
	if r.gpuMonitor != nil {
		gpuInfo, err = r.gpuMonitor.GetGPUInfo()
		if err != nil {
			logger.Error("Failed to get GPU information", zap.Error(err))
			// Continue with nil GPU info
			gpuInfo = nil
		}
	} else {
		logger.Debug("GPU monitor not available, skipping GPU information")
	}

	// Get available models from Ollama
	models, err := r.ollamaClient.ListModels(ctx)
	if err != nil {
		logger.Error("Failed to get models from Ollama", zap.Error(err), zap.String("llm_url", r.config.Provider.LLMURL))
		// Instead of returning an error, continue with an empty models list
		models = &ollama.ListModelsResponse{
			Object: "list",
			Models: []ollama.OllamaModel{},
		}
	}

	// Create health report
	report := HealthReport{
		Object: "list",
		Data:   models.Models,
		GPU:    gpuInfo,
		NGROK: map[string]interface{}{
			"url": r.config.NGROK.URL,
		},
		ProviderType: r.config.Provider.ProviderType,
	}

	// Marshal report to JSON
	reportJSON, err := json.Marshal(report)
	if err != nil {
		logger.Error("Failed to marshal health report", zap.Error(err))
		return fmt.Errorf("failed to marshal health report: %w", err)
	}

	// Create request
	url := fmt.Sprintf("%s/api/provider/health", r.config.Provider.URL)
	if gpuInfo != nil {
		logger.Debug("Sending health report",
			zap.String("url", url),
			zap.Int("models_count", len(models.Models)),
			zap.String("gpu", gpuInfo.ProductName))
	} else {
		logger.Debug("Sending health report",
			zap.String("url", url),
			zap.Int("models_count", len(models.Models)),
			zap.String("gpu", "none"))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reportJSON))
	if err != nil {
		logger.Error("Failed to create request", zap.Error(err))
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.config.Provider.APIKey))

	// Send request
	resp, err := r.client.Do(req)
	if err != nil {
		logger.Error("Failed to send health report", zap.Error(err))
		return fmt.Errorf("failed to send health report: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Read response body for error details
		body, _ := io.ReadAll(resp.Body)
		logger.Error("Health report failed",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(body)))
		return fmt.Errorf("health report failed with status code: %d, response: %s", resp.StatusCode, string(body))
	}

	// Update the last update time
	r.lastUpdateMutex.Lock()
	r.lastUpdateTime = time.Now()
	r.lastUpdateMutex.Unlock()

	logger.Info("Health report sent successfully",
		zap.Time("timestamp", r.lastUpdateTime),
		zap.Int("models_count", len(models.Models)))

	return nil
}

// GetHealthReport returns the current health report
func (r *Reporter) GetHealthReport(ctx context.Context) (*HealthReport, error) {
	logger.Debug("Getting health report")

	// Get GPU information if available
	var gpuInfo *gpu.GPUInfo
	var err error
	if r.gpuMonitor != nil {
		gpuInfo, err = r.gpuMonitor.GetGPUInfo()
		if err != nil {
			logger.Error("Failed to get GPU information", zap.Error(err))
			// Continue with nil GPU info rather than returning an error
			gpuInfo = nil
		}
	} else {
		logger.Debug("GPU monitor not available, skipping GPU information")
	}

	// Get available models from Ollama
	models, err := r.ollamaClient.ListModels(ctx)
	if err != nil {
		logger.Error("Failed to get models from Ollama", zap.Error(err), zap.String("llm_url", r.config.Provider.LLMURL))
		// Instead of returning an error, continue with an empty models list
		models = &ollama.ListModelsResponse{
			Object: "list",
			Models: []ollama.OllamaModel{},
		}
	}

	// Create health report
	report := &HealthReport{
		Object: "list",
		Data:   models.Models,
		GPU:    gpuInfo,
		NGROK: map[string]interface{}{
			"url": r.config.NGROK.URL,
		},
		ProviderType: r.config.Provider.ProviderType,
	}

	// Add null check for GPU info before logging
	if gpuInfo != nil {
		logger.Debug("Health report generated",
			zap.Int("models_count", len(models.Models)),
			zap.String("gpu", gpuInfo.ProductName))
	} else {
		logger.Debug("Health report generated",
			zap.Int("models_count", len(models.Models)),
			zap.String("gpu", "none"))
	}

	return report, nil
}

// GetLastUpdateTime returns the time of the last successful health report
func (r *Reporter) GetLastUpdateTime() time.Time {
	r.lastUpdateMutex.Lock()
	defer r.lastUpdateMutex.Unlock()
	return r.lastUpdateTime
}
