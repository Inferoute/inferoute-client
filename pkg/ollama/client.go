package ollama

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

// Client is a client for the Ollama API
type Client struct {
	baseURL string
	client  *http.Client
}

// OllamaModel represents a model in the Ollama API
type OllamaModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ListModelsResponse represents the response from the Ollama API for listing models
type ListModelsResponse struct {
	Object string        `json:"object"`
	Models []OllamaModel `json:"data"`
}

// ChatMessage represents a message in a chat conversation
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a request to the Ollama chat API
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// ChatResponse represents a response from the Ollama chat API
type ChatResponse struct {
	Model     string      `json:"model"`
	Message   ChatMessage `json:"message"`
	CreatedAt time.Time   `json:"created_at"`
	Done      bool        `json:"done"`
	Usage     TokenUsage  `json:"usage,omitempty"`
}

// TokenUsage represents token usage information
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// NewClient creates a new Ollama client
func NewClient(baseURL string) *Client {
	logger.Debug("Creating new Ollama client", zap.String("base_url", baseURL))
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ListModels lists all available models
func (c *Client) ListModels(ctx context.Context) (*ListModelsResponse, error) {
	url := fmt.Sprintf("%s/v1/models", c.baseURL)
	logger.Debug("Listing Ollama models", zap.String("url", url))

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Error("Failed to create request for listing models", zap.Error(err))
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// DOCKER DEBUG: Pre-request state
	logger.Debug("About to send request",
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.Bool("context_canceled", ctx.Err() == context.Canceled),
		zap.Bool("context_timeout", ctx.Err() == context.DeadlineExceeded))

	// Send request
	resp, err := c.client.Do(req)

	// DOCKER DEBUG: Post-request state
	logger.Debug("Request completed",
		zap.Bool("resp_nil", resp == nil),
		zap.Error(err),
		zap.Any("context_error", ctx.Err()))

	if err != nil {
		logger.Error("Failed to send request for listing models",
			zap.Error(err),
			zap.String("url", url),
			zap.Bool("context_done", ctx.Err() != nil))
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// DOCKER DEBUG: Response details
	logger.Debug("Got response",
		zap.Int("status_code", resp.StatusCode),
		zap.String("status", resp.Status))

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("Unexpected status code",
			zap.Int("status_code", resp.StatusCode),
			zap.String("body", string(body)))
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse response
	var response ListModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logger.Error("Failed to parse response", zap.Error(err))
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Debug("Successfully listed models", zap.Int("count", len(response.Models)))
	return &response, nil
}

// Chat sends a chat request to the Ollama API
func (c *Client) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	url := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	logger.Debug("Sending chat request to Ollama",
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
