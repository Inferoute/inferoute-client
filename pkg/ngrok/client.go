package ngrok

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

// Client represents a client for interacting with the NGROK API
type Client struct {
	httpClient *http.Client
	apiPort    int
}

// TunnelsResponse represents the response from the NGROK API tunnels endpoint
type TunnelsResponse struct {
	Tunnels []Tunnel `json:"tunnels"`
}

// Tunnel represents a NGROK tunnel
type Tunnel struct {
	Name      string `json:"name"`
	PublicURL string `json:"public_url"`
	Proto     string `json:"proto"`
}

// NewClient creates a new NGROK client
func NewClient(apiPort int) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		apiPort: apiPort,
	}
}

// GetPublicURL fetches the current NGROK public URL from the local API
func (c *Client) GetPublicURL() (string, error) {
	url := fmt.Sprintf("http://localhost:%d/api/tunnels", c.apiPort)

	logger.Debug("Fetching NGROK public URL", zap.String("url", url))

	resp, err := c.httpClient.Get(url)
	if err != nil {
		logger.Error("Failed to connect to NGROK API", zap.Error(err))
		return "", fmt.Errorf("failed to connect to NGROK API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("NGROK API returned non-OK status", zap.Int("status_code", resp.StatusCode))
		return "", fmt.Errorf("NGROK API returned status code: %d", resp.StatusCode)
	}

	var tunnelsResp TunnelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tunnelsResp); err != nil {
		logger.Error("Failed to decode NGROK API response", zap.Error(err))
		return "", fmt.Errorf("failed to decode NGROK API response: %w", err)
	}

	// Find the first HTTP/HTTPS tunnel
	for _, tunnel := range tunnelsResp.Tunnels {
		if tunnel.Proto == "https" || tunnel.Proto == "http" {
			logger.Debug("Found NGROK public URL", zap.String("url", tunnel.PublicURL))
			return tunnel.PublicURL, nil
		}
	}

	logger.Error("No HTTP/HTTPS tunnels found in NGROK API response")
	return "", fmt.Errorf("no HTTP/HTTPS tunnels found in NGROK API response")
}
