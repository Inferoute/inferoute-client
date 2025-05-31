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
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/cloudflare"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/pricing"
	"go.uber.org/zap"
)

// Reporter handles health reporting to the central system
type Reporter struct {
	config           *config.Config
	gpuMonitor       *gpu.Monitor
	llmClient        llm.Client
	cloudflareClient *cloudflare.Client
	pricingClient    *pricing.Client
	client           *http.Client
	lastUpdateTime   time.Time
	lastUpdateMutex  sync.Mutex
	// Track registered models to avoid duplicate registrations
	registeredModels   map[string]bool
	registeredModelsMu sync.Mutex
}

// HealthReport represents a health report to be sent to the central system
type HealthReport struct {
	Object       string                 `json:"object"`
	Data         []llm.Model            `json:"data"`
	GPU          *gpu.GPUInfo           `json:"gpu"`
	Cloudflare   map[string]interface{} `json:"cloudflare"`
	ProviderType string                 `json:"provider_type"`
}

// NewReporter creates a new health reporter
func NewReporter(cfg *config.Config, gpuMonitor *gpu.Monitor, llmClient llm.Client) *Reporter {
	// Create Cloudflare client using provider API key
	cloudflareClient := cloudflare.NewClient(cfg.Provider.URL, cfg.Provider.APIKey, cfg.Cloudflare.ServiceURL)

	// Create pricing client
	pricingClient := pricing.NewClient(cfg.Provider.URL, cfg.Provider.APIKey)

	return &Reporter{
		config:           cfg,
		gpuMonitor:       gpuMonitor,
		llmClient:        llmClient,
		cloudflareClient: cloudflareClient,
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
func (r *Reporter) registerNewModels(ctx context.Context, models []llm.Model) {
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
	// Get health report
	report, err := r.GetHealthReport(ctx)
	if err != nil {
		return fmt.Errorf("failed to get health report: %w", err)
	}

	// Register any new models
	r.registerNewModels(ctx, report.Data)

	// Log the Cloudflare section of the report before sending
	logger.Info("Preparing to send health report with Cloudflare info",
		zap.Any("cloudflare_section", report.Cloudflare))

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

// GetHealthReport gets the current health report
func (r *Reporter) GetHealthReport(ctx context.Context) (*HealthReport, error) {
	// Get list of models
	models, err := r.llmClient.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	// Get GPU info if available
	var gpuInfo *gpu.GPUInfo
	if r.gpuMonitor != nil {
		gpuInfo, err = r.gpuMonitor.GetGPUInfo()
		if err != nil {
			logger.Error("Failed to get GPU information", zap.Error(err))
			gpuInfo = &gpu.GPUInfo{
				ProductName:   "Unknown",
				DriverVersion: "Unknown",
				CUDAVersion:   "Unknown",
				GPUCount:      0,
			}
		}
	} else {
		// No GPU monitor available
		gpuInfo = &gpu.GPUInfo{
			ProductName:   "Unknown",
			DriverVersion: "Unknown",
			CUDAVersion:   "Unknown",
			GPUCount:      0,
		}
	}

	// Get Cloudflare info if available
	var cloudflareInfo map[string]interface{}
	if r.cloudflareClient != nil {
		tunnelURL := r.cloudflareClient.GetTunnelURL()
		hostname := r.cloudflareClient.GetHostname()
		isRunning := r.cloudflareClient.IsRunning()

		logger.Info("Gathering Cloudflare information for health report",
			zap.String("tunnel_url", tunnelURL),
			zap.String("hostname", hostname),
			zap.Bool("is_running", isRunning))

		if tunnelURL != "" {
			cloudflareInfo = map[string]interface{}{
				"url": tunnelURL,
			}
			logger.Info("Cloudflare info will be included in health report",
				zap.Any("cloudflare_info", cloudflareInfo))
		} else {
			logger.Warn("No Cloudflare tunnel URL available for health report")
		}
	} else {
		logger.Warn("No Cloudflare client available for health report")
	}

	// Create report
	report := &HealthReport{
		Object:       "list",
		Data:         models.Models,
		GPU:          gpuInfo,
		Cloudflare:   cloudflareInfo,
		ProviderType: r.config.Provider.ProviderType,
	}

	return report, nil
}

// GetLastUpdateTime gets the time of the last successful health update
func (r *Reporter) GetLastUpdateTime() time.Time {
	r.lastUpdateMutex.Lock()
	defer r.lastUpdateMutex.Unlock()
	return r.lastUpdateTime
}

// SetCloudflareClient updates the Cloudflare client used by the health reporter
func (r *Reporter) SetCloudflareClient(client *cloudflare.Client) {
	r.cloudflareClient = client
}
