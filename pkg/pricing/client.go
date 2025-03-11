package pricing

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

type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

type ModelPrice struct {
	ModelName      string  `json:"model_name"`
	AvgInputPrice  float64 `json:"avg_input_price"`
	AvgOutputPrice float64 `json:"avg_output_price"`
	SampleSize     int     `json:"sample_size"`
}

type GetPricesRequest struct {
	Models []string `json:"models"`
}

type GetPricesResponse struct {
	ModelPrices []ModelPrice `json:"model_prices"`
}

type RegisterModelRequest struct {
	ModelName         string  `json:"model_name"`
	ServiceType       string  `json:"service_type"`
	InputPriceTokens  float64 `json:"input_price_tokens"`
	OutputPriceTokens float64 `json:"output_price_tokens"`
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	Message    string `json:"error"`
	StatusCode int    `json:"-"` // Not part of the JSON response, added for context
}

// Error implements the error interface
func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) GetModelPrices(ctx context.Context, models []string) (*GetPricesResponse, error) {
	url := fmt.Sprintf("%s/api/model-pricing/get-prices", c.baseURL)
	logger.Debug("Getting model prices", zap.Strings("models", models))

	request := GetPricesRequest{Models: models}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	var response GetPricesResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &response, nil
}

func (c *Client) RegisterModel(ctx context.Context, model string, serviceType string, inputPrice, outputPrice float64) error {
	url := fmt.Sprintf("%s/api/provider/models", c.baseURL)
	logger.Debug("Registering model",
		zap.String("model", model),
		zap.String("service_type", serviceType),
		zap.Float64("input_price", inputPrice),
		zap.Float64("output_price", outputPrice))

	request := RegisterModelRequest{
		ModelName:         model,
		ServiceType:       serviceType,
		InputPriceTokens:  inputPrice,
		OutputPriceTokens: outputPrice,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(requestJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for error responses
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			// If we can't parse the error response, return a generic error
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
		}
		errResp.StatusCode = resp.StatusCode
		return &errResp
	}

	return nil
}
