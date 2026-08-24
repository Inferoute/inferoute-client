package llm

import (
	"context"
	"time"
)

// FileMeasurement is one hashed weight file included in health reports.
type FileMeasurement struct {
	Name       string `json:"name"`
	Hash       string `json:"hash"`
	HashMethod string `json:"hash_method"`
	Size       int64  `json:"size"`
}

// Model represents a model in the LLM API
type Model struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           int64                  `json:"created"`
	OwnedBy           string                 `json:"owned_by"`
	Digest            string                 `json:"digest,omitempty"`
	SizeBytes         int64                  `json:"size_bytes,omitempty"`
	WeightFingerprint string                 `json:"weight_fingerprint,omitempty"`
	ServiceType       string                 `json:"service_type,omitempty"`
	HFRepo            string                 `json:"hf_repo,omitempty"`
	HFRevision        string                 `json:"hf_revision,omitempty"`
	Files             []FileMeasurement      `json:"files,omitempty"`
	Details           map[string]interface{} `json:"details,omitempty"`
	ClientVersion     string                 `json:"client_version,omitempty"`
}

// ListModelsResponse represents the response from the LLM API for listing models
type ListModelsResponse struct {
	Object string  `json:"object"`
	Models []Model `json:"data"`
}

// ChatMessage represents a message in a chat conversation
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a request to the LLM chat API
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// ChatResponse represents a response from the LLM chat API
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

// Client interface defines the methods that any LLM client must implement
type Client interface {
	// ListModels lists all available models
	ListModels(ctx context.Context) (*ListModelsResponse, error)

	// Chat sends a chat request to the LLM API
	Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error)

	// ForwardRequest forwards a raw request to the LLM API
	// This is used for direct forwarding of OpenAI-compatible requests
	ForwardRequest(ctx context.Context, path string, body []byte) ([]byte, error)
}

// NewClient creates a new LLM client based on the provider type
func NewClient(providerType string, baseURL string) Client {
	switch providerType {
	case "vllm":
		return NewVLLMClient(baseURL)
	default:
		return NewOllamaClient(baseURL)
	}
}
