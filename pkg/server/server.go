package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ollama"
)

// Server represents the HTTP server
type Server struct {
	config         *config.Config
	gpuMonitor     *gpu.Monitor
	healthReporter *health.Reporter
	ollamaClient   *ollama.Client
	server         *http.Server
}

// NewServer creates a new server
func NewServer(cfg *config.Config, gpuMonitor *gpu.Monitor, healthReporter *health.Reporter) *Server {
	return &Server{
		config:         cfg,
		gpuMonitor:     gpuMonitor,
		healthReporter: healthReporter,
		ollamaClient:   ollama.NewClient(cfg.Provider.LLMURL),
	}
}

// Start starts the server
func (s *Server) Start() error {
	// Create router
	r := mux.NewRouter()

	// Register routes
	r.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet)
	r.HandleFunc("/busy", s.handleBusy).Methods(http.MethodGet)
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

	// Start server
	log.Printf("Starting server on %s:%d", s.config.Server.Host, s.config.Server.Port)
	return s.server.ListenAndServe()
}

// Stop stops the server
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// handleHealth handles the /health endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Get health report
	report, err := s.healthReporter.GetHealthReport(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get health report: %v", err), http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// handleBusy handles the /busy endpoint
func (s *Server) handleBusy(w http.ResponseWriter, r *http.Request) {
	// Check if GPU is busy
	isBusy, err := s.gpuMonitor.IsBusy()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"busy": isBusy})
}

// handleChatCompletions handles the /v1/chat/completions endpoint
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// Check if GPU is busy
	isBusy, err := s.gpuMonitor.IsBusy()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
		return
	}

	// If GPU is busy, return error
	if isBusy {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "GPU is busy"})
		return
	}

	// Validate HMAC
	hmac := r.Header.Get("X-Inferoute-HMAC")
	if hmac != "" {
		if err := s.validateHMAC(r.Context(), hmac); err != nil {
			http.Error(w, fmt.Sprintf("Invalid HMAC: %v", err), http.StatusUnauthorized)
			return
		}
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Forward request to Ollama
	ollamaResp, err := s.forwardToOllama(r.Context(), "/v1/chat/completions", body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to forward request to Ollama: %v", err), http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Write(ollamaResp)
}

// handleCompletions handles the /v1/completions endpoint
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	// Check if GPU is busy
	isBusy, err := s.gpuMonitor.IsBusy()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
		return
	}

	// If GPU is busy, return error
	if isBusy {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "GPU is busy"})
		return
	}

	// Validate HMAC
	hmac := r.Header.Get("X-Inferoute-HMAC")
	if hmac != "" {
		if err := s.validateHMAC(r.Context(), hmac); err != nil {
			http.Error(w, fmt.Sprintf("Invalid HMAC: %v", err), http.StatusUnauthorized)
			return
		}
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Forward request to Ollama
	ollamaResp, err := s.forwardToOllama(r.Context(), "/v1/completions", body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to forward request to Ollama: %v", err), http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Write(ollamaResp)
}

// validateHMAC validates an HMAC with the central system
func (s *Server) validateHMAC(ctx context.Context, hmac string) error {
	// Create request
	url := fmt.Sprintf("%s/api/provider/validate_hmac", s.config.Provider.URL)
	reqBody, err := json.Marshal(map[string]string{"hmac": hmac})
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
		return fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	// Parse response
	var response struct {
		Valid bool `json:"valid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
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
