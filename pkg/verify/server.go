package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ServerClient calls provider-authenticated verification APIs.
type ServerClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewServerClient creates a client for POST /api/provider/verify-model.
func NewServerClient(baseURL, apiKey string) *ServerClient {
	return &ServerClient{
		baseURL: stringsTrimRightSlash(baseURL),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// VerifyModel submits measurements; the server returns verification status only.
func (s *ServerClient) VerifyModel(ctx context.Context, req verifyModelRequest) (verifyModelResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return verifyModelResponse{}, err
	}

	url := fmt.Sprintf("%s/api/provider/verify-model", s.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return verifyModelResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.apiKey))

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return verifyModelResponse{}, fmt.Errorf("verify model: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return verifyModelResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return verifyModelResponse{}, fmt.Errorf("verify model returned %d: %s", resp.StatusCode, string(respBody))
	}

	var out verifyModelResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return verifyModelResponse{}, fmt.Errorf("decode verify model response: %w", err)
	}
	return out, nil
}
