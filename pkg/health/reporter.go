package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ollama"
)

// Reporter handles health reporting to the central system
type Reporter struct {
	config     *config.Config
	gpuMonitor *gpu.Monitor
	client     *http.Client
}

// HealthReport represents a health report to be sent to the central system
type HealthReport struct {
	Object string                 `json:"object"`
	Data   []ollama.OllamaModel   `json:"data"`
	GPU    *gpu.GPUInfo           `json:"gpu"`
	NGROK  map[string]interface{} `json:"ngrok"`
}

// NewReporter creates a new health reporter
func NewReporter(cfg *config.Config, gpuMonitor *gpu.Monitor) *Reporter {
	return &Reporter{
		config:     cfg,
		gpuMonitor: gpuMonitor,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// SendHealthReport sends a health report to the central system
func (r *Reporter) SendHealthReport(ctx context.Context) error {
	// Get GPU information
	gpuInfo, err := r.gpuMonitor.GetGPUInfo()
	if err != nil {
		return fmt.Errorf("failed to get GPU information: %w", err)
	}

	// Get available models from Ollama
	ollamaClient := ollama.NewClient(r.config.Ollama.URL)
	models, err := ollamaClient.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get models from Ollama: %w", err)
	}

	// Create health report
	report := HealthReport{
		Object: "list",
		Data:   models.Models,
		GPU:    gpuInfo,
		NGROK: map[string]interface{}{
			"url": r.config.NGROK.URL,
		},
	}

	// Marshal report to JSON
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal health report: %w", err)
	}

	// Log the JSON payload
	fmt.Printf("Sending health report JSON: %s\n", string(reportJSON))

	// Create request
	url := fmt.Sprintf("%s/api/provider/health", r.config.Provider.URL)
	fmt.Printf("Sending health report to URL: %s\n", url)
	fmt.Printf("Using API key: %s\n", r.config.Provider.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reportJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.config.Provider.APIKey))

	// Send request
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send health report: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Read response body for error details
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health report failed with status code: %d, response: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetHealthReport returns the current health report
func (r *Reporter) GetHealthReport(ctx context.Context) (*HealthReport, error) {
	// Get GPU information
	gpuInfo, err := r.gpuMonitor.GetGPUInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get GPU information: %w", err)
	}

	// Get available models from Ollama
	ollamaClient := ollama.NewClient(r.config.Ollama.URL)
	models, err := ollamaClient.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get models from Ollama: %w", err)
	}

	// Create health report
	report := &HealthReport{
		Object: "list",
		Data:   models.Models,
		GPU:    gpuInfo,
		NGROK: map[string]interface{}{
			"url": r.config.NGROK.URL,
		},
	}

	return report, nil
}
