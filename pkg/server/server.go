package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ngrok"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ollama"
	"go.uber.org/zap"
)

// rename this file to server.go since it contains the same methods
// the original server.go file seems to have been renamed to service.go
// but there are still references to server.go in the codebase
// this is causing duplicate method declarations

// maskString Helper function to mask sensitive strings
func maskStringHelper(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// Creates a new server
func CreateServer(cfg *config.Config, gpuMonitor *gpu.Monitor, healthReporter *health.Reporter, ollamaClient *ollama.Client) *Server {
	// Create NGROK client
	ngrokClient := ngrok.NewClient(cfg.NGROK.Port)

	return &Server{
		config:         cfg,
		gpuMonitor:     gpuMonitor,
		healthReporter: healthReporter,
		ollamaClient:   ollamaClient,
		ngrokClient:    ngrokClient,
		errorLog:       make([]string, 0, 100),
	}
}

// Start starts the server
func (s *Server) Start() error {
	// Try to fetch the NGROK URL on startup
	if s.ngrokClient != nil {
		ngrokURL, err := s.ngrokClient.GetPublicURL()
		if err != nil {
			logger.Warn("Failed to fetch NGROK URL on startup", zap.Error(err))
		} else {
			// Update the config with the fetched URL
			s.config.NGROK.URL = ngrokURL
			logger.Info("NGROK URL fetched on startup", zap.String("url", ngrokURL))
		}
	}

	// Create router
	r := mux.NewRouter()

	// Register routes
	r.HandleFunc("/api/health", s.handleHealth).Methods(http.MethodGet)
	r.HandleFunc("/api/busy", s.handleBusy).Methods(http.MethodGet)
	r.HandleFunc("/v1/chat/completions", s.handleChatCompletions).Methods(http.MethodPost)
	r.HandleFunc("/v1/completions", s.handleCompletions).Methods(http.MethodPost)

	// Create server
	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Print startup banner with GPU info
	s.printStartupBanner()

	// Start a goroutine to periodically update the console
	go s.consoleUpdater()

	// Log server start
	logger.Info("Starting HTTP server",
		zap.String("address", s.server.Addr),
		zap.String("provider_type", s.config.Provider.ProviderType),
		zap.String("llm_url", s.config.Provider.LLMURL))

	// Start server
	return s.server.ListenAndServe()
}

// Stop stops the server
func (s *Server) Stop(ctx context.Context) error {
	logger.Info("Stopping HTTP server")
	return s.server.Shutdown(ctx)
}

// consoleUpdater periodically updates the console with request stats and errors
func (s *Server) consoleUpdater() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Create a debug log file
	debugFile, err := os.OpenFile("debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		logger.Error("Failed to open debug log file", zap.Error(err))
	} else {
		defer debugFile.Close()
	}

	for range ticker.C {
		s.redrawConsole()
	}
}

// redrawConsole completely redraws the console
func (s *Server) redrawConsole() {
	// Get GPU info if available
	var gpuInfo *gpu.GPUInfo
	if s.gpuMonitor != nil {
		var err error
		gpuInfo, err = s.gpuMonitor.GetGPUInfo()
		if err != nil {
			s.logError(fmt.Sprintf("Failed to get GPU info: %v", err))
			gpuInfo = nil
		}
	}

	// If GPU info is nil, use a default
	if gpuInfo == nil {
		gpuInfo = &gpu.GPUInfo{
			ProductName:   "Unknown",
			DriverVersion: "Unknown",
			CUDAVersion:   "Unknown",
			GPUCount:      0,
		}
	}

	// Create a buffer to build the output
	var buf bytes.Buffer

	// Clear screen
	buf.WriteString("\033[H\033[2J")

	// Print banner
	buf.WriteString("\033[1;36m╔════════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║                     INFEROUTE PROVIDER CLIENT                    ║\n")
	buf.WriteString("╚════════════════════════════════════════════════════════════════╝\033[0m\n")

	// Get last health update time
	lastUpdate := s.healthReporter.GetLastUpdateTime()
	lastUpdateStr := "Never"
	if !lastUpdate.IsZero() {
		lastUpdateStr = lastUpdate.Format("2006-01-02 15:04:05")
	}
	buf.WriteString(fmt.Sprintf("\033[1;35mLast Health Update            \033[0m%s\n", lastUpdateStr))
	buf.WriteString("\033[1;35mSession Status                \033[0m\033[1;32monline\033[0m\n")
	buf.WriteString(fmt.Sprintf("\033[1;35mProvider Type                 \033[0m%s\n", s.config.Provider.ProviderType))
	buf.WriteString(fmt.Sprintf("\033[1;35mProvider API Key              \033[0m%s\n", maskStringHelper(s.config.Provider.APIKey)))
	buf.WriteString(fmt.Sprintf("\033[1;35mProvider URL                  \033[0m%s\n", s.config.Provider.URL))
	buf.WriteString(fmt.Sprintf("\033[1;35mLLM URL                       \033[0m%s\n", s.config.Provider.LLMURL))
	buf.WriteString(fmt.Sprintf("\033[1;35mWeb Interface                 \033[0m\033[4mhttp://%s:%d\033[0m\n", s.config.Server.Host, s.config.Server.Port))
	if s.config.NGROK.URL != "" {
		buf.WriteString(fmt.Sprintf("\033[1;35mNGROK URL                     \033[0m%s\n", s.config.NGROK.URL))
	}

	buf.WriteString("\033[1;36m╔════════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║                          GPU INFORMATION                         ║\n")
	buf.WriteString("╚════════════════════════════════════════════════════════════════╝\033[0m\n")

	buf.WriteString(fmt.Sprintf("\033[1;35mGPU                          \033[0m%s\n", gpuInfo.ProductName))
	buf.WriteString(fmt.Sprintf("\033[1;35mDriver Version               \033[0m%s\n", gpuInfo.DriverVersion))
	buf.WriteString(fmt.Sprintf("\033[1;35mCUDA Version                 \033[0m%s\n", gpuInfo.CUDAVersion))
	buf.WriteString(fmt.Sprintf("\033[1;35mGPU Count                    \033[0m%d\n", gpuInfo.GPUCount))

	// Print last 10 requests
	buf.WriteString("\n\033[1;33mRecent Requests:\033[0m\n")
	s.requestStats.mutex.Lock()
	if len(s.requestStats.LastRequests) == 0 {
		buf.WriteString("No requests yet\n")
	} else {
		for _, req := range s.requestStats.LastRequests {
			buf.WriteString(req + "\n")
		}
	}
	s.requestStats.mutex.Unlock()

	// Print errors section if there are any
	s.errorLogMutex.Lock()
	if len(s.errorLog) > 0 {
		buf.WriteString("\n\033[1;31mErrors:\033[0m\n")
		for _, err := range s.errorLog {
			buf.WriteString(err + "\n")
		}
	}
	s.errorLogMutex.Unlock()

	// Write the entire buffer to stdout at once
	fmt.Print(buf.String())
}

// printStartupBanner prints a nice startup banner with GPU info
func (s *Server) printStartupBanner() {
	// Just use the redrawConsole method to avoid duplication
	s.redrawConsole()
}

// logRequest logs a request to the console
func (s *Server) logRequest(method, path string, statusCode int, startTime time.Time) {
	duration := time.Since(startTime)

	// Format the log entry
	var statusColor string
	switch {
	case statusCode >= 200 && statusCode < 300:
		statusColor = "\033[1;32m" // Green
		s.requestStats.mutex.Lock()
		s.requestStats.Success++
		s.requestStats.mutex.Unlock()
	case statusCode == 401:
		statusColor = "\033[1;33m" // Yellow
		s.requestStats.mutex.Lock()
		s.requestStats.Unauthorized++
		s.requestStats.mutex.Unlock()
	default:
		statusColor = "\033[1;31m" // Red
		s.requestStats.mutex.Lock()
		s.requestStats.Errors++
		s.requestStats.mutex.Unlock()
	}

	// Format duration with only 2 decimal places
	var durationStr string
	if duration.Seconds() >= 1 {
		durationStr = fmt.Sprintf("%.2fs", duration.Seconds())
	} else {
		durationStr = fmt.Sprintf("%.2fms", float64(duration.Microseconds())/1000)
	}

	timestamp := time.Now().Format("15:04:05.000")
	logEntry := fmt.Sprintf("%s UTC %s %s %s%d\033[0m %s",
		timestamp, method, path, statusColor, statusCode, durationStr)

	// Add to request stats
	s.requestStats.mutex.Lock()
	s.requestStats.Total++
	s.requestStats.LastRequests = append(s.requestStats.LastRequests, logEntry)
	if len(s.requestStats.LastRequests) > 10 {
		s.requestStats.LastRequests = s.requestStats.LastRequests[1:]
	}
	s.requestStats.mutex.Unlock()

	// Log to zap logger
	logger.Info("Request processed",
		zap.String("method", method),
		zap.String("path", path),
		zap.Int("status", statusCode),
		zap.Duration("duration", duration))
}

// logError logs an error to the error log
func (s *Server) logError(errMsg string) {
	timestamp := time.Now().Format("15:04:05.000")
	logEntry := fmt.Sprintf("%s ERROR: %s", timestamp, errMsg)

	// Add to error log
	s.errorLogMutex.Lock()
	s.errorLog = append(s.errorLog, logEntry)
	if len(s.errorLog) > 10 {
		s.errorLog = s.errorLog[1:]
	}
	s.errorLogMutex.Unlock()

	// Log to zap logger
	logger.Error(errMsg)
}

// validateHMAC validates an HMAC with the central system
func (s *Server) validateHMAC(ctx context.Context, hmac string) error {
	// Create request
	url := fmt.Sprintf("%s/api/provider/validate_hmac", s.config.Provider.URL)

	reqBody, err := json.Marshal(HMACValidationRequest{HMAC: hmac})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.config.Provider.APIKey))

	// Send request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Read and log response body for debugging
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr == nil {
			s.logError(fmt.Sprintf("HMAC validation error response: %s", string(respBody)))
		}
		return fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	// Parse response
	var response HMACValidationResponse
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if HMAC is valid
	if !response.Valid {
		return fmt.Errorf("invalid HMAC")
	}

	return nil
}

// forwardToOllama forwards a request to Ollama
func (s *Server) forwardToOllama(ctx context.Context, path string, body []byte) ([]byte, error) {
	// Create request
	url := fmt.Sprintf("%s%s", s.config.Provider.LLMURL, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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
