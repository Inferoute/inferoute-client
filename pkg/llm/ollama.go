package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

// OllamaClient implements the LLM Client interface for Ollama
type OllamaClient struct {
	baseURL string
	client  *http.Client
}

// OllamaModel represents the Ollama-specific model format
type OllamaModel struct {
	Name       string                 `json:"name"`
	Model      string                 `json:"model"`
	ModifiedAt string                 `json:"modified_at"`
	Size       int64                  `json:"size"`
	Digest     string                 `json:"digest"`
	Details    map[string]interface{} `json:"details"`
}

// OllamaListModelsResponse represents the Ollama-specific response format
type OllamaListModelsResponse struct {
	Models []OllamaModel `json:"models"`
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(baseURL string) Client {
	logger.Debug("Creating new Ollama client", zap.String("base_url", baseURL))
	return &OllamaClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ListModels lists all available models
func (c *OllamaClient) ListModels(ctx context.Context) (*ListModelsResponse, error) {
	url := fmt.Sprintf("%s/api/tags", c.baseURL)
	logger.Debug("Listing Ollama models", zap.String("url", url))

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Error("Failed to create request for listing models", zap.Error(err))
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		logger.Error("Failed to send request for listing models", zap.Error(err), zap.String("url", url))
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		logger.Error("Request failed for listing models",
			zap.Int("status_code", resp.StatusCode),
			zap.String("url", url))
		return nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	// Parse response into Ollama format
	var ollamaResponse OllamaListModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResponse); err != nil {
		logger.Error("Failed to parse response for listing models", zap.Error(err))
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to standard format
	response := &ListModelsResponse{
		Models: make([]Model, len(ollamaResponse.Models)),
	}

	for i, ollamaModel := range ollamaResponse.Models {
		format := "unknown"
		if details, ok := ollamaModel.Details["format"]; ok {
			format = fmt.Sprintf("%v", details)
		}

		response.Models[i] = Model{
			ID:      fmt.Sprintf("%s/%s", format, ollamaModel.Model),
			Object:  "model",
			Created: time.Now().Unix(), // Using current time as Created is not provided
			OwnedBy: "ollama",
		}
	}

	logger.Debug("Successfully listed Ollama models",
		zap.Int("model_count", len(response.Models)),
		zap.String("url", url))
	return response, nil
}

// Chat sends a chat request to the Ollama API
func (c *OllamaClient) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	// Strip out gguf/ prefix if present for Ollama compatibility
	modelName := request.Model
	if strings.HasPrefix(modelName, "gguf/") {
		modelName = strings.TrimPrefix(modelName, "gguf/")
		logger.Debug("Stripped gguf/ prefix from model name for Ollama compatibility",
			zap.String("original_model", request.Model),
			zap.String("ollama_model", modelName))
	}

	url := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	logger.Debug("Sending chat request to Ollama",
		zap.String("url", url),
		zap.String("model", modelName),
		zap.Int("message_count", len(request.Messages)),
		zap.Bool("stream", request.Stream))

	// Create a copy of the request with the modified model name
	ollamaRequest := *request
	ollamaRequest.Model = modelName

	// Marshal request to JSON
	requestJSON, err := json.Marshal(ollamaRequest)
	if err != nil {
		logger.Error("Failed to marshal chat request", zap.Error(err))
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(requestJSON))
	if err != nil {
		logger.Error("Failed to create chat request", zap.Error(err))
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		logger.Error("Failed to send chat request",
			zap.Error(err),
			zap.String("url", url),
			zap.String("model", request.Model))
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		logger.Error("Chat request failed",
			zap.Int("status_code", resp.StatusCode),
			zap.String("url", url),
			zap.String("model", request.Model))
		return nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	// Parse response
	var response ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logger.Error("Failed to parse chat response", zap.Error(err))
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Debug("Successfully received chat response",
		zap.String("model", response.Model),
		zap.Bool("done", response.Done),
		zap.Int("prompt_tokens", response.Usage.PromptTokens),
		zap.Int("completion_tokens", response.Usage.CompletionTokens))
	return &response, nil
}

// ForwardRequest forwards a raw request to the Ollama API
func (c *OllamaClient) ForwardRequest(ctx context.Context, path string, body []byte) ([]byte, error) {
	url := fmt.Sprintf("%s%s", c.baseURL, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, nil
}
