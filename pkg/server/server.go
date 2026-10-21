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

	"errors"

	"github.com/gorilla/mux"
	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/connect"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/usermsg"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
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
func CreateServer(cfg *config.Config, gpuMonitor *gpu.Monitor, healthReporter *health.Reporter, verifier *verify.Verifier) *Server {
	llmClient := llm.NewClient(cfg.Provider.ProviderType, cfg.Provider.LLMURL, cfg.LLMTimeout())

	s := &Server{
		config:           cfg,
		gpuMonitor:       gpuMonitor,
		healthReporter:   healthReporter,
		llmClient:        llmClient,
		verifier:         verifier,
		consoleUI:        true,
		maxInflight:      int32(cfg.Server.MaxConcurrentInference),
		sessionQueueWait: cfg.SessionQueueWait(),
		errorLog:         make([]string, 0, 100),
	}

	// Health reports include the inflight-slot state in is_busy so the
	// central busy flag reflects actual serving, not just GPU utilization.
	if healthReporter != nil {
		healthReporter.SetSlotBusyFunc(s.slotsBusy)
	}

	return s
}

// SetConsoleUI enables or disables the ANSI terminal dashboard.
// Windows tray mode turns this off so the process can hide its console.
func (s *Server) SetConsoleUI(enabled bool) {
	s.consoleUI = enabled
}

// Start starts the local dashboard and the outbound provider connection.
func (s *Server) Start() error {
	connectCtx, connectCancel := context.WithCancel(context.Background())
	s.connectCancel = connectCancel
	s.connector = connect.New(s.config.Provider.URL, s.config.Provider.APIKey, s.inferenceMux(), s.onConnect)
	go func() {
		if err := s.connector.Run(connectCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("provider connection stopped", zap.Error(err))
		}
	}()
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := s.connector.Ready(readyCtx)
	readyCancel()
	if errors.Is(err, connect.ErrInvalidAPIKey) {
		connectCancel()
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Create router
	r := mux.NewRouter()

	// Register routes
	r.HandleFunc("/", s.handleDashboard).Methods(http.MethodGet)
	r.HandleFunc("/api/status", s.handleStatus).Methods(http.MethodGet)
	r.HandleFunc("/api/health", s.handleHealth).Methods(http.MethodGet)
	r.HandleFunc("/api/busy", s.handleBusy).Methods(http.MethodGet)
	r.HandleFunc("/v1/chat/completions", s.handleChatCompletions).Methods(http.MethodPost)
	r.HandleFunc("/v1/completions", s.handleCompletions).Methods(http.MethodPost)
	r.HandleFunc("/v1/embeddings", s.handleEmbeddings).Methods(http.MethodPost)

	// Create server. Read/write timeouts must cover the session queue wait
	// plus inference so the orchestrator's sticky timeout is usable.
	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port),
		Handler:      r,
		ReadTimeout:  s.config.RequestTimeout(),
		WriteTimeout: s.config.RequestTimeout(),
		IdleTimeout:  120 * time.Second,
	}

	if s.consoleUI {
		s.printStartupBanner()
		go s.consoleUpdater()
	}

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
	if s.connectCancel != nil {
		s.connectCancel()
	}

	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) inferenceMux() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/v1/chat/completions", s.handleChatCompletions).Methods(http.MethodPost)
	r.HandleFunc("/v1/completions", s.handleCompletions).Methods(http.MethodPost)
	r.HandleFunc("/v1/embeddings", s.handleEmbeddings).Methods(http.MethodPost)
	return r
}

func (s *Server) onConnect() {
	if s.healthReporter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.healthReporter.SendHealthReport(ctx); err != nil {
		logger.Error("health report after connect failed", zap.Error(err))
	}
}

func (s *Server) gatewayConnected() bool {
	return s.connector != nil && s.connector.Connected()
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
	snap := s.snapshot()

	var buf bytes.Buffer
	buf.WriteString("\033[H\033[2J")
	buf.WriteString("\033[1;36m╔════════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║                     INFEROUTE PROVIDER CLIENT                    ║\n")
	buf.WriteString("╚════════════════════════════════════════════════════════════════╝\033[0m\n")

	buf.WriteString(fmt.Sprintf("\033[1;35mLast Health Update            \033[0m%s\n", snap.LastHealthUpdate))
	buf.WriteString("\033[1;35mSession Status                \033[0m\033[1;32monline\033[0m\n")
	switch snap.UpdateStatus {
	case "required":
		buf.WriteString(fmt.Sprintf("\033[1;31mUpdate required: %s -> %s. Run 'inferoute-client update' then restart.\033[0m\n", snap.ClientVersion, snap.LatestVersion))
	case "available":
		buf.WriteString(fmt.Sprintf("\033[1;33mUpdate available: %s -> %s. Run 'inferoute-client update' then restart.\033[0m\n", snap.ClientVersion, snap.LatestVersion))
	}
	buf.WriteString(fmt.Sprintf("\033[1;35mProvider Type                 \033[0m%s\n", snap.ProviderType))
	buf.WriteString(fmt.Sprintf("\033[1;35mProvider API Key              \033[0m%s\n", snap.ProviderAPIKey))
	buf.WriteString(fmt.Sprintf("\033[1;35mProvider URL                  \033[0m%s\n", snap.ProviderURL))
	buf.WriteString(fmt.Sprintf("\033[1;35mLLM URL                       \033[0m%s\n", snap.LLMURL))
	state := "connecting"
	if snap.Connected {
		state = "connected"
	}
	buf.WriteString(fmt.Sprintf("\033[1;35mPlatform connection           \033[0m%s\n", state))

	s.writeModelStatus(&buf, snap.Models)

	gpuInfo := snap.GPU
	if gpuInfo == nil {
		gpuInfo = &gpu.GPUInfo{ProductName: "Unknown", DriverVersion: "Unknown", CUDAVersion: "Unknown"}
	}
	buf.WriteString("\033[1;36m╔════════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║                          GPU INFORMATION                         ║\n")
	buf.WriteString("╚════════════════════════════════════════════════════════════════╝\033[0m\n")
	buf.WriteString(fmt.Sprintf("\033[1;35mGPU                          \033[0m%s\n", gpuInfo.ProductName))
	buf.WriteString(fmt.Sprintf("\033[1;35mDriver Version               \033[0m%s\n", gpuInfo.DriverVersion))
	buf.WriteString(fmt.Sprintf("\033[1;35mCUDA Version                 \033[0m%s\n", gpuInfo.CUDAVersion))
	buf.WriteString(fmt.Sprintf("\033[1;35mGPU Count                    \033[0m%d\n", gpuInfo.GPUCount))

	buf.WriteString("\n\033[1;33mRecent Requests:\033[0m\n")
	if len(snap.RecentRequests) == 0 {
		buf.WriteString("No requests yet\n")
	} else {
		for _, req := range snap.RecentRequests {
			buf.WriteString(formatRequestLine(req) + "\n")
		}
	}

	if len(snap.Errors) > 0 {
		buf.WriteString("\n\033[1;31mErrors:\033[0m\n")
		for _, err := range snap.Errors {
			buf.WriteString(err + "\n")
		}
	}

	fmt.Print(buf.String())
}

func formatRequestLine(req RequestLog) string {
	var statusColor string
	switch {
	case req.Status >= 200 && req.Status < 300:
		statusColor = "\033[1;32m"
	case req.Status == 401:
		statusColor = "\033[1;33m"
	default:
		statusColor = "\033[1;31m"
	}
	return fmt.Sprintf("%s UTC %s %s %s%d\033[0m %s",
		req.Time, req.Method, req.Path, statusColor, req.Status, req.Duration)
}

func (s *Server) writeModelStatus(buf *bytes.Buffer, models []StatusModel) {
	buf.WriteString("\033[1;36m╔════════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║                           MODEL STATUS                           ║\n")
	buf.WriteString("╚════════════════════════════════════════════════════════════════╝\033[0m\n")

	if len(models) == 0 {
		buf.WriteString("\033[1;35mModel                         \033[0m\033[1;33m(awaiting health sync)\033[0m\n")
		return
	}

	for i, m := range models {
		prefix := "Model                         "
		if i > 0 {
			prefix = "                                "
		}
		_, color := usermsg.ApprovalConsole(m.VerificationStatus)
		buf.WriteString(fmt.Sprintf("\033[1;35m%s\033[0m%s\n", prefix, m.ID))
		buf.WriteString(fmt.Sprintf("\033[1;35mMarketplace approval          \033[0m%s%s\033[0m\n", color, m.MarketplaceApproval))
	}
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
	switch {
	case statusCode >= 200 && statusCode < 300:
		s.requestStats.mutex.Lock()
		s.requestStats.Success++
		s.requestStats.mutex.Unlock()
	case statusCode == 401:
		s.requestStats.mutex.Lock()
		s.requestStats.Unauthorized++
		s.requestStats.mutex.Unlock()
	default:
		s.requestStats.mutex.Lock()
		s.requestStats.Errors++
		s.requestStats.mutex.Unlock()
	}

	timestamp := time.Now().Format("15:04:05.000")
	entry := RequestLog{
		Time:     timestamp,
		Method:   method,
		Path:     path,
		Status:   statusCode,
		Duration: formatDuration(duration),
	}

	s.requestStats.mutex.Lock()
	s.requestStats.Total++
	s.requestStats.LastRequests = append(s.requestStats.LastRequests, entry)
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
	client := nodeapi.NewHTTPClient(10 * time.Second)
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

func (s *Server) verifyModelInRequest(ctx context.Context, body []byte) error {
	if s.verifier == nil {
		return nil
	}
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Model == "" {
		return fmt.Errorf("missing model in request")
	}
	return s.verifier.CheckInference(ctx, s.llmClient, payload.Model)
}

// forwardToLLM forwards a request to the LLM provider
func (s *Server) forwardToLLM(ctx context.Context, path string, body []byte) (*http.Response, error) {
	return s.llmClient.ForwardRequest(ctx, path, body)
}
