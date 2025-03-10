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

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ngrok"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ollama"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/pricing"
	"go.uber.org/zap"
)

// Reporter handles health reporting to the central system
type Reporter struct {
	config          *config.Config
	gpuMonitor      *gpu.Monitor
	ollamaClient    *ollama.Client
	ngrokClient     *ngrok.Client
	pricingClient   *pricing.Client
	client          *http.Client
	lastUpdateTime  time.Time
	lastUpdateMutex sync.Mutex
	// Track registered models to avoid duplicate registrations
	registeredModels   map[string]bool
	registeredModelsMu sync.Mutex
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
	// Create NGROK client
	ngrokClient := ngrok.NewClient(cfg.NGROK.Port)

	// Create pricing client
	pricingClient := pricing.NewClient(cfg.Provider.URL, cfg.Provider.APIKey)

	return &Reporter{
		config:           cfg,
		gpuMonitor:       gpuMonitor,
		ollamaClient:     ollamaClient,
		ngrokClient:      ngrokClient,
		pricingClient:    pricingClient,
		client:           &http.Client{Timeout: 10 * time.Second},
		registeredModels: make(map[string]bool),
	}
}

// InitializeRegisteredModels initializes the set of registered models
// This should be called after the initial model registration at startup
func (r *Reporter) InitializeRegisteredModels(modelIDs []string) {
	r.registeredModelsMu.Lock()
	defer r.registeredModelsMu.Unlock()

	for _, id := range modelIDs {
		r.registeredModels[id] = true
	}

	logger.Info("Initialized registered models tracker",
		zap.Int("model_count", len(modelIDs)))
}

// registerNewModels checks for new models and registers them with pricing
func (r *Reporter) registerNewModels(ctx context.Context, models []ollama.OllamaModel) {
	// Get the current list of model IDs
	currentModelIDs := make([]string, 0, len(models))
	for _, model := range models {
		currentModelIDs = append(currentModelIDs, model.ID)
	}

	// Find new models that need to be registered
	newModels := make([]string, 0)
	r.registeredModelsMu.Lock()
	for _, id := range currentModelIDs {
		if !r.registeredModels[id] {
			newModels = append(newModels, id)
		}
	}
	r.registeredModelsMu.Unlock()

	if len(newModels) == 0 {
		return // No new models to register
	}

	logger.Info("Found new models to register",
		zap.Strings("new_models", newModels))

	// Get pricing for the new models
	prices, err := r.pricingClient.GetModelPrices(ctx, newModels)
	if err != nil {
		logger.Error("Failed to get model prices for new models",
			zap.Error(err),
			zap.Strings("models", newModels))
		return
	}

	// Create a map of model prices for easy lookup and find default pricing
	priceMap := make(map[string]pricing.ModelPrice)
	var defaultPrice pricing.ModelPrice
	for _, price := range prices.ModelPrices {
		if price.ModelName == "default" {
			defaultPrice = price
			continue
		}
		priceMap[price.ModelName] = price
	}

	if defaultPrice.ModelName == "" {
		logger.Warn("No default pricing found in API response, using hardcoded defaults")
		defaultPrice = pricing.ModelPrice{
			ModelName:      "default",
			AvgInputPrice:  0.0002,
			AvgOutputPrice: 0.0003,
		}
	}

	// Register each new model
	for _, modelName := range newModels {
		price, exists := priceMap[modelName]
		if !exists {
			logger.Info("No specific pricing found for model, using default pricing",
				zap.String("model", modelName),
				zap.Float64("default_input_price", defaultPrice.AvgInputPrice),
				zap.Float64("default_output_price", defaultPrice.AvgOutputPrice))

			err := r.pricingClient.RegisterModel(ctx, modelName, r.config.Provider.ProviderType,
				defaultPrice.AvgInputPrice, defaultPrice.AvgOutputPrice)

			if err != nil {
				// Check if it's a 400 error (model already exists)
				if resp, ok := err.(*pricing.ErrorResponse); ok && resp.StatusCode == 400 {
					logger.Info("Model already registered elsewhere",
						zap.String("model", modelName))

					// Still mark it as registered in our tracker
					r.registeredModelsMu.Lock()
					r.registeredModels[modelName] = true
					r.registeredModelsMu.Unlock()
					continue
				}

				logger.Error("Failed to register model with default pricing",
					zap.String("model", modelName),
					zap.Error(err))
				continue
			}
		} else {
			logger.Info("Registering model with specific pricing",
				zap.String("model", modelName),
				zap.Float64("input_price", price.AvgInputPrice),
				zap.Float64("output_price", price.AvgOutputPrice),
				zap.Int("sample_size", price.SampleSize))

			err := r.pricingClient.RegisterModel(ctx, modelName, r.config.Provider.ProviderType,
				price.AvgInputPrice, price.AvgOutputPrice)

			if err != nil {
				// Check if it's a 400 error (model already exists)
				if resp, ok := err.(*pricing.ErrorResponse); ok && resp.StatusCode == 400 {
					logger.Info("Model already registered elsewhere",
						zap.String("model", modelName))

					// Still mark it as registered in our tracker
					r.registeredModelsMu.Lock()
					r.registeredModels[modelName] = true
					r.registeredModelsMu.Unlock()
					continue
				}

				logger.Error("Failed to register model",
					zap.String("model", modelName),
					zap.Error(err))
				continue
			}
		}

		// Mark model as registered
		r.registeredModelsMu.Lock()
		r.registeredModels[modelName] = true
		r.registeredModelsMu.Unlock()

		logger.Info("Successfully registered new model",
			zap.String("model", modelName))
	}
}

// SendHealthReport sends a health report to the central system
func (r *Reporter) SendHealthReport(ctx context.Context) error {
	logger.Debug("Preparing health report")

	// Get the current NGROK URL for the health report
	var ngrokURL string
	if r.ngrokClient != nil {
		var err error
		ngrokURL, err = r.ngrokClient.GetPublicURL()
		if err != nil {
			logger.Warn("Failed to fetch NGROK URL for health report", zap.Error(err))
			// Continue without NGROK URL
		} else {
			logger.Debug("Using NGROK URL for health report", zap.String("url", ngrokURL))
		}
	}

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

	// Check for and register new models
	r.registerNewModels(ctx, models.Models)

	// Create health report
	report := HealthReport{
		Object: "list",
		Data:   models.Models,
		GPU:    gpuInfo,
		NGROK: map[string]interface{}{
			"url": ngrokURL,
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

	// Get the current NGROK URL for the health report
	var ngrokURL string
	if r.ngrokClient != nil {
		var err error
		ngrokURL, err = r.ngrokClient.GetPublicURL()
		if err != nil {
			logger.Warn("Failed to fetch NGROK URL for health report", zap.Error(err))
			// Continue without NGROK URL
		} else {
			logger.Debug("Using NGROK URL for health report", zap.String("url", ngrokURL))
		}
	}

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
			"url": ngrokURL,
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
