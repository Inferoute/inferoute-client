package cloudflare

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	appLogger "github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

// cloudflaredLogger is a separate logger for cloudflared output that goes to files only
var cloudflaredLogger *zap.Logger

func init() {
	// Create a cloudflared-specific logger that only writes to files (no stdout)
	config := zap.NewProductionConfig()
	config.OutputPaths = []string{}      // No stdout
	config.ErrorOutputPaths = []string{} // No stderr

	// This will be overridden once we have access to the main logger config
	logger, _ := config.Build()
	cloudflaredLogger = logger
}

// initCloudflaredLogger initializes the cloudflared logger using the main app logger config
func initCloudflaredLogger() {
	// Get the default logger and create a child logger for cloudflared
	defaultLogger := appLogger.GetDefaultLogger()
	cloudflaredLogger = defaultLogger.Named("cloudflared")
}

// Client represents a production-grade cloudflared tunnel client with supervision
type Client struct {
	httpClient  *http.Client
	coreURL     string
	bearerToken string
	serviceURL  string

	// Runtime state
	token    string
	hostname string
	cmd      *exec.Cmd
	process  *os.Process

	// Control and monitoring
	ctx              context.Context
	cancel           context.CancelFunc
	monitoringCtx    context.Context
	monitoringCancel context.CancelFunc
	restartCh        chan struct{}
	shutdownCh       chan struct{}

	// State management
	mu            sync.RWMutex
	running       bool
	shouldRestart bool
	restartCount  int
	lastRestart   time.Time
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

// NewClient creates a new supervised Cloudflare client
func NewClient(coreURL, bearerToken, serviceURL string) *Client {
	// Initialize cloudflared logger
	initCloudflaredLogger()

	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		coreURL:       coreURL,
		bearerToken:   bearerToken,
		serviceURL:    serviceURL,
		restartCh:     make(chan struct{}, 1),
		shutdownCh:    make(chan struct{}),
		shouldRestart: true,
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

	appLogger.Debug("Requesting Cloudflare tunnel",
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
		appLogger.Error("Failed to request Cloudflare tunnel", zap.Error(err))
		return fmt.Errorf("failed to request tunnel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		appLogger.Error("Cloudflare tunnel API returned non-OK status", zap.Int("status_code", resp.StatusCode))
		return fmt.Errorf("tunnel API returned status code: %d", resp.StatusCode)
	}

	var tunnelResp TunnelResponse
	if err := json.NewDecoder(resp.Body).Decode(&tunnelResp); err != nil {
		appLogger.Error("Failed to decode tunnel response", zap.Error(err))
		return fmt.Errorf("failed to decode tunnel response: %w", err)
	}

	c.token = tunnelResp.Token
	c.hostname = tunnelResp.Hostname

	appLogger.Info("Cloudflare tunnel requested successfully",
		zap.String("hostname", c.hostname))

	return nil
}

// StartTunnel starts the cloudflared process with comprehensive supervision
func (c *Client) StartTunnel(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token == "" {
		return fmt.Errorf("no tunnel token available, call RequestTunnel first")
	}

	if c.running {
		return fmt.Errorf("tunnel is already running")
	}

	// Create contexts for tunnel and monitoring
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.monitoringCtx, c.monitoringCancel = context.WithCancel(ctx)

	// Start the monitoring goroutine
	go c.supervisionLoop()

	// Start the tunnel for the first time
	if err := c.startTunnelProcess(); err != nil {
		c.cancel()
		c.monitoringCancel()
		return fmt.Errorf("failed to start initial tunnel process: %w", err)
	}

	// Monitor context cancellation that might kill cloudflared
	go func() {
		<-c.ctx.Done()
		appLogger.Warn("Cloudflared context was cancelled",
			zap.Error(c.ctx.Err()),
			zap.String("reason", "context_cancellation"))
	}()

	c.running = true
	appLogger.Info("Cloudflare tunnel supervision started", zap.String("hostname", c.hostname))

	return nil
}

// startTunnelProcess starts the actual cloudflared process
func (c *Client) startTunnelProcess() error {
	appLogger.Info("Starting cloudflared process", zap.String("hostname", c.hostname))

	// Create the command with cloudflared's own logging to debug the issue
	// NOT using CommandContext to test if context cancellation is the issue
	c.cmd = exec.Command("cloudflared", "tunnel", "run",
		"--token", c.token,
		"--logfile", "/tmp/cloudflared-debug.log", // Let cloudflared log to its own file
		"--loglevel", "debug", // Maximum cloudflared logging
	)

	// Remove process group setting that might interfere
	// c.cmd.SysProcAttr = &syscall.SysProcAttr{
	//     Setpgid: true, // This might be causing issues
	// }

	// Capture stdout and stderr to see what's happening
	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := c.cmd.Start(); err != nil {
		appLogger.Error("Failed to start cloudflared process", zap.Error(err))
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	c.process = c.cmd.Process

	appLogger.Info("Cloudflared process started",
		zap.Int("pid", c.process.Pid),
		zap.String("hostname", c.hostname),
		zap.String("debug_log", "/tmp/cloudflared-debug.log"))

	// Log cloudflared output in real-time
	go c.logOutput("stdout", stdoutPipe)
	go c.logOutput("stderr", stderrPipe)

	// Monitor the process exit
	go c.monitorProcessExit()

	// Wait a bit longer for startup and check multiple times
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if !c.isProcessRunning() {
			return fmt.Errorf("cloudflared process died during startup (attempt %d/10)", i+1)
		}
	}

	appLogger.Info("Cloudflared process started successfully",
		zap.String("hostname", c.hostname),
		zap.Int("pid", c.process.Pid))

	return nil
}

// logOutput captures and logs cloudflared output to files only
func (c *Client) logOutput(stream string, pipe io.ReadCloser) {
	defer pipe.Close()

	// Initialize cloudflared logger if not done yet
	if cloudflaredLogger == nil {
		initCloudflaredLogger()
	}

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()

		// Categorize log levels based on content
		lower := strings.ToLower(line)

		// Determine log level based on content
		if strings.Contains(lower, "fatal") ||
			(strings.Contains(lower, "error") && !strings.Contains(lower, "wrn") && !strings.Contains(lower, "warn")) ||
			(strings.Contains(lower, "failed") && !strings.Contains(lower, "wrn") && !strings.Contains(lower, "warn")) {
			cloudflaredLogger.Error("cloudflared error",
				zap.String("stream", stream),
				zap.String("output", line))
		} else if strings.Contains(lower, "wrn") || strings.Contains(lower, "warn") || strings.Contains(lower, "warning") {
			cloudflaredLogger.Warn("cloudflared warning",
				zap.String("stream", stream),
				zap.String("output", line))
		} else if strings.Contains(lower, "registered tunnel connection") ||
			strings.Contains(lower, "updated to new configuration") ||
			strings.Contains(lower, "starting metrics server") {
			// Important connection events as info
			cloudflaredLogger.Info("cloudflared connection",
				zap.String("stream", stream),
				zap.String("output", line))
		} else {
			// Everything else as debug (to keep files clean)
			cloudflaredLogger.Debug("cloudflared output",
				zap.String("stream", stream),
				zap.String("output", line))
		}
	}

	if err := scanner.Err(); err != nil {
		cloudflaredLogger.Error("Error reading cloudflared output",
			zap.String("stream", stream),
			zap.Error(err))
	}
}

// monitorProcessExit monitors when the process exits and logs the reason
func (c *Client) monitorProcessExit() {
	if c.cmd == nil {
		return
	}

	// Wait for the process to exit
	err := c.cmd.Wait()

	c.mu.RLock()
	shouldRestart := c.shouldRestart
	c.mu.RUnlock()

	// Get exit code for better diagnostics
	exitCode := -1
	if exitError, ok := err.(*exec.ExitError); ok {
		exitCode = exitError.ExitCode()
	}

	if err != nil {
		appLogger.Error("Cloudflared process exited with error",
			zap.Error(err),
			zap.Int("exit_code", exitCode),
			zap.Bool("should_restart", shouldRestart))

		// Specific exit code handling
		switch exitCode {
		case -1:
			if strings.Contains(err.Error(), "signal: killed") {
				appLogger.Error("Process was forcefully killed (SIGKILL) - external termination detected")
				// This suggests something else is killing our process
			} else if strings.Contains(err.Error(), "signal: terminated") {
				appLogger.Warn("Process was terminated (SIGTERM) - likely graceful shutdown")
			}
		case 1:
			appLogger.Warn("Exit code 1: Likely token expiration or authentication failure")
		case 2:
			appLogger.Warn("Exit code 2: Likely configuration error")
		case 130:
			appLogger.Info("Exit code 130: Process interrupted (SIGINT)")
		case 143:
			appLogger.Info("Exit code 143: Process terminated (SIGTERM)")
		default:
			appLogger.Warn("Unknown exit code", zap.Int("code", exitCode))
		}
	} else {
		appLogger.Info("Cloudflared process exited normally",
			zap.Bool("should_restart", shouldRestart))
	}

	// If we should restart, trigger it
	if shouldRestart {
		select {
		case c.restartCh <- struct{}{}:
			appLogger.Info("Triggered restart due to process exit", zap.Int("exit_code", exitCode))
		default:
			appLogger.Debug("Restart already queued")
		}
	}
}

// supervisionLoop continuously monitors and restarts the tunnel process
func (c *Client) supervisionLoop() {
	ticker := time.NewTicker(600 * time.Second) // Health check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-c.monitoringCtx.Done():
			appLogger.Info("Supervision loop terminating")
			return

		case <-c.shutdownCh:
			appLogger.Info("Received shutdown signal")
			return

		case <-c.restartCh:
			c.handleRestart()

		case <-ticker.C:
			c.healthCheck()
		}
	}
}

// healthCheck monitors the process health and triggers restart if needed
func (c *Client) healthCheck() {
	c.mu.RLock()
	shouldRestart := c.shouldRestart
	c.mu.RUnlock()

	if !shouldRestart {
		return
	}

	if !c.isProcessRunning() {
		appLogger.Warn("Cloudflared process died, triggering restart")
		select {
		case c.restartCh <- struct{}{}:
		default:
			// Channel full, restart already queued
		}
	}
}

// handleRestart manages the restart logic with exponential backoff
func (c *Client) handleRestart() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.shouldRestart {
		appLogger.Debug("Restart requested but shouldRestart is false, ignoring")
		return
	}

	// Implement exponential backoff
	now := time.Now()
	if now.Sub(c.lastRestart) < time.Minute {
		c.restartCount++
	} else {
		c.restartCount = 0
	}
	c.lastRestart = now

	// Calculate backoff delay
	delay := time.Duration(c.restartCount) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}

	if delay > 0 {
		appLogger.Info("Backing off before restart",
			zap.Duration("delay", delay),
			zap.Int("restart_count", c.restartCount))

		time.Sleep(delay)
	}

	// Clean up old process
	c.cleanupProcess()

	// Request a fresh token before restarting (in case old token expired)
	appLogger.Info("Requesting fresh token before restart")
	if err := c.RequestTunnel(context.Background()); err != nil {
		appLogger.Error("Failed to get fresh token for restart", zap.Error(err))
		// Continue with old token as fallback
	} else {
		appLogger.Info("Got fresh token for restart", zap.String("hostname", c.hostname))
	}

	// Start new process
	if err := c.startTunnelProcess(); err != nil {
		appLogger.Error("Failed to restart cloudflared process",
			zap.Error(err),
			zap.Int("restart_count", c.restartCount))

		// Schedule another restart attempt
		go func() {
			time.Sleep(5 * time.Second)
			select {
			case c.restartCh <- struct{}{}:
			default:
			}
		}()
	} else {
		appLogger.Info("Cloudflared process restarted successfully",
			zap.String("hostname", c.hostname),
			zap.Int("restart_count", c.restartCount))
	}
}

// isProcessRunning checks if the cloudflared process is still alive
func (c *Client) isProcessRunning() bool {
	if c.process == nil {
		return false
	}

	// Check if process is still alive by sending signal 0
	err := c.process.Signal(syscall.Signal(0))
	return err == nil
}

// cleanupProcess properly terminates and cleans up the current process
func (c *Client) cleanupProcess() {
	if c.process == nil {
		return
	}

	appLogger.Debug("Cleaning up cloudflared process", zap.Int("pid", c.process.Pid))

	// Try graceful termination first
	if err := c.process.Signal(syscall.SIGTERM); err != nil {
		appLogger.Warn("Failed to send SIGTERM", zap.Error(err))
	}

	// Wait for graceful shutdown
	done := make(chan error, 1)
	go func() {
		_, err := c.process.Wait()
		done <- err
	}()

	select {
	case <-time.After(10 * time.Second):
		// Force kill if it doesn't stop gracefully
		appLogger.Warn("Process didn't terminate gracefully, force killing")
		if err := c.process.Kill(); err != nil {
			appLogger.Error("Failed to force kill process", zap.Error(err))
		}
		c.process.Wait()
	case err := <-done:
		if err != nil {
			appLogger.Debug("Process exited with error", zap.Error(err))
		}
	}

	c.process = nil
	c.cmd = nil
}

// StopTunnel stops the cloudflared process and supervision
func (c *Client) StopTunnel() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	appLogger.Info("Stopping cloudflared tunnel supervision")

	// Stop restart behavior
	c.shouldRestart = false

	// Signal monitoring to stop
	if c.monitoringCancel != nil {
		c.monitoringCancel()
	}

	// Signal shutdown
	select {
	case c.shutdownCh <- struct{}{}:
	default:
	}

	// Cancel tunnel context
	if c.cancel != nil {
		c.cancel()
	}

	// Clean up the current process
	c.cleanupProcess()

	c.running = false
	appLogger.Info("Cloudflare tunnel stopped")
	return nil
}

// RestartTunnel manually triggers a tunnel restart
func (c *Client) RestartTunnel() error {
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()

	if !running {
		return fmt.Errorf("tunnel is not running")
	}

	appLogger.Info("Manual tunnel restart requested")
	select {
	case c.restartCh <- struct{}{}:
		return nil
	default:
		return fmt.Errorf("restart already queued")
	}
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

// IsRunning checks if the tunnel supervision is active
func (c *Client) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running && c.isProcessRunning()
}

// GetStatus returns detailed tunnel status information
func (c *Client) GetStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := map[string]interface{}{
		"supervision_active": c.running,
		"process_running":    c.isProcessRunning(),
		"hostname":           c.hostname,
		"url":                c.GetTunnelURL(),
		"restart_count":      c.restartCount,
		"should_restart":     c.shouldRestart,
	}

	if c.process != nil {
		status["pid"] = c.process.Pid
	}

	if !c.lastRestart.IsZero() {
		status["last_restart"] = c.lastRestart.Format(time.RFC3339)
	}

	return status
}
