package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

// VLLMClient implements the LLM Client interface for vLLM
type VLLMClient struct {
	baseURL string
	client  *http.Client
}

// NewVLLMClient creates a new vLLM client
func NewVLLMClient(baseURL string) Client {
	logger.Debug("Creating new vLLM client", zap.String("base_url", baseURL))
	return &VLLMClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ListModels lists all available models
func (c *VLLMClient) ListModels(ctx context.Context) (*ListModelsResponse, error) {
	url := fmt.Sprintf("%s/v1/models", c.baseURL)
	logger.Debug("Listing vLLM models", zap.String("url", url))

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

	// Parse response
	var response ListModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logger.Error("Failed to parse response for listing models", zap.Error(err))
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Debug("Successfully listed vLLM models",
		zap.Int("model_count", len(response.Models)),
		zap.String("url", url))
	return &response, nil
}

// Chat sends a chat request to the vLLM API
func (c *VLLMClient) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	logger.Debug("Sending chat request to vLLM",
		zap.String("url", url),
		zap.String("model", request.Model),
		zap.Int("message_count", len(request.Messages)),
		zap.Bool("stream", request.Stream))

	// Marshal request to JSON
	requestJSON, err := json.Marshal(request)
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

// ForwardRequest forwards a raw request to the vLLM API
func (c *VLLMClient) ForwardRequest(ctx context.Context, path string, body []byte) ([]byte, error) {
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
