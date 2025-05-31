package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

// Client represents a client for interacting with Cloudflare tunnels
type Client struct {
	httpClient  *http.Client
	coreURL     string
	bearerToken string
	serviceURL  string

	// Runtime state
	token    string
	hostname string
	process  *os.Process
}

// TunnelRequest represents the request to create a tunnel
type TunnelRequest struct {
	ServiceURL string `json:"service_url"`
}

// TunnelResponse represents the response from the tunnel creation API
type TunnelResponse struct {
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
}

// NewClient creates a new Cloudflare client
func NewClient(coreURL, bearerToken, serviceURL string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		coreURL:     coreURL,
		bearerToken: bearerToken,
		serviceURL:  serviceURL,
	}
}

// RequestTunnel requests a new tunnel from the core system
func (c *Client) RequestTunnel(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/cloudflare/tunnel/request", c.coreURL)

	reqBody := TunnelRequest{
		ServiceURL: c.serviceURL,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	logger.Debug("Requesting Cloudflare tunnel",
		zap.String("url", url),
		zap.String("service_url", c.serviceURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.bearerToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("Failed to request Cloudflare tunnel", zap.Error(err))
		return fmt.Errorf("failed to request tunnel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("Cloudflare tunnel API returned non-OK status", zap.Int("status_code", resp.StatusCode))
		return fmt.Errorf("tunnel API returned status code: %d", resp.StatusCode)
	}

	var tunnelResp TunnelResponse
	if err := json.NewDecoder(resp.Body).Decode(&tunnelResp); err != nil {
		logger.Error("Failed to decode tunnel response", zap.Error(err))
		return fmt.Errorf("failed to decode tunnel response: %w", err)
	}

	c.token = tunnelResp.Token
	c.hostname = tunnelResp.Hostname

	logger.Info("Cloudflare tunnel requested successfully",
		zap.String("hostname", c.hostname))

	return nil
}

// StartTunnel starts the cloudflared process with the obtained token
func (c *Client) StartTunnel(ctx context.Context) error {
	if c.token == "" {
		return fmt.Errorf("no tunnel token available, call RequestTunnel first")
	}

	// Stop existing tunnel if running
	if err := c.StopTunnel(); err != nil {
		logger.Warn("Failed to stop existing tunnel", zap.Error(err))
	}

	logger.Info("Starting cloudflared tunnel", zap.String("hostname", c.hostname))

	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel", "run", "--token", c.token)

	// Start the process
	if err := cmd.Start(); err != nil {
		logger.Error("Failed to start cloudflared", zap.Error(err))
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	c.process = cmd.Process

	// Give it a moment to start
	time.Sleep(2 * time.Second)

	// Check if process is still running
	if c.process != nil {
		if err := c.process.Signal(syscall.Signal(0)); err != nil {
			logger.Error("Cloudflared process died after start", zap.Error(err))
			return fmt.Errorf("cloudflared process died: %w", err)
		}
	}

	logger.Info("Cloudflared tunnel started successfully", zap.String("hostname", c.hostname))
	return nil
}

// StopTunnel stops the cloudflared process
func (c *Client) StopTunnel() error {
	if c.process == nil {
		return nil
	}

	logger.Info("Stopping cloudflared tunnel")

	// Send SIGTERM first
	if err := c.process.Signal(syscall.SIGTERM); err != nil {
		logger.Warn("Failed to send SIGTERM to cloudflared", zap.Error(err))
	}

	// Give it time to shut down gracefully
	done := make(chan error, 1)
	go func() {
		_, err := c.process.Wait()
		done <- err
	}()

	select {
	case <-time.After(5 * time.Second):
		// Force kill if it doesn't stop gracefully
		logger.Warn("Cloudflared didn't stop gracefully, force killing")
		if err := c.process.Kill(); err != nil {
			logger.Error("Failed to kill cloudflared", zap.Error(err))
			return err
		}
		c.process.Wait()
	case err := <-done:
		if err != nil {
			logger.Warn("Cloudflared exited with error", zap.Error(err))
		}
	}

	c.process = nil
	logger.Info("Cloudflared tunnel stopped")
	return nil
}

// GetHostname returns the current tunnel hostname
func (c *Client) GetHostname() string {
	return c.hostname
}

// GetTunnelURL returns the full tunnel URL (with https prefix)
func (c *Client) GetTunnelURL() string {
	if c.hostname == "" {
		return ""
	}
	return fmt.Sprintf("https://%s", c.hostname)
}

// IsRunning checks if the cloudflared process is still running
func (c *Client) IsRunning() bool {
	if c.process == nil {
		return false
	}

	// Check if process is still alive
	err := c.process.Signal(syscall.Signal(0))
	return err == nil
}
