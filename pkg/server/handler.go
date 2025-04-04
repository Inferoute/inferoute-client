package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// handleHealth handles the /api/health endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Get health report
	report, err := s.healthReporter.GetHealthReport(r.Context())
	if err != nil {
		s.logError(fmt.Sprintf("Failed to get health report: %v", err))
		http.Error(w, fmt.Sprintf("Failed to get health report: %v", err), http.StatusInternalServerError)
		s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
	s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
}

// handleBusy handles the /api/busy endpoint
func (s *Server) handleBusy(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Check if GPU is busy
	var isBusy bool
	var err error

	if s.gpuMonitor != nil {
		isBusy, err = s.gpuMonitor.IsBusy()
		if err != nil {
			s.logError(fmt.Sprintf("Error checking if GPU is busy: %v", err))
			http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
			s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
			return
		}
	} else {
		// If GPU monitor is not available, assume not busy
		isBusy = false
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BusyResponse{Busy: isBusy})
	s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
}

// handleChatCompletions handles the /v1/chat/completions endpoint
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Check if GPU is busy
	var isBusy bool
	var err error

	if s.gpuMonitor != nil {
		isBusy, err = s.gpuMonitor.IsBusy()
		if err != nil {
			s.logError(fmt.Sprintf("Error checking if GPU is busy: %v", err))
			http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
			s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
			return
		}
	} else {
		// If GPU monitor is not available, assume not busy
		isBusy = false
	}

	// If GPU is busy, return error
	if isBusy {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "GPU is busy"})
		s.logRequest(r.Method, r.URL.Path, http.StatusServiceUnavailable, startTime)
		return
	}

	// Validate HMAC from X-Request-Id header
	hmac := r.Header.Get("X-Request-Id")
	if hmac != "" {
		if err := s.validateHMAC(r.Context(), hmac); err != nil {
			s.logError(fmt.Sprintf("HMAC validation failed: %v", err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Invalid HMAC: %v", err)})
			s.logRequest(r.Method, r.URL.Path, http.StatusUnauthorized, startTime)
			return
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Missing HMAC in X-Request-Id header"})
		s.logRequest(r.Method, r.URL.Path, http.StatusUnauthorized, startTime)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to read request body: %v", err))
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		s.logRequest(r.Method, r.URL.Path, http.StatusBadRequest, startTime)
		return
	}

	// Forward request to LLM provider
	llmResp, err := s.forwardToLLM(r.Context(), "/v1/chat/completions", body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to forward request to LLM provider: %v", err))
		http.Error(w, fmt.Sprintf("Failed to forward request to LLM provider: %v", err), http.StatusInternalServerError)
		s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Write(llmResp)
	s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
}

// handleCompletions handles the /v1/completions endpoint
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Check if GPU is busy
	var isBusy bool
	var err error

	if s.gpuMonitor != nil {
		isBusy, err = s.gpuMonitor.IsBusy()
		if err != nil {
			s.logError(fmt.Sprintf("Error checking if GPU is busy: %v", err))
			http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
			s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
			return
		}
	} else {
		// If GPU monitor is not available, assume not busy
		isBusy = false
	}

	// If GPU is busy, return error
	if isBusy {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "GPU is busy"})
		s.logRequest(r.Method, r.URL.Path, http.StatusServiceUnavailable, startTime)
		return
	}

	// Validate HMAC from X-Request-Id header
	hmac := r.Header.Get("X-Request-Id")
	if hmac != "" {
		if err := s.validateHMAC(r.Context(), hmac); err != nil {
			s.logError(fmt.Sprintf("HMAC validation failed: %v", err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Invalid HMAC: %v", err)})
			s.logRequest(r.Method, r.URL.Path, http.StatusUnauthorized, startTime)
			return
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Missing HMAC in X-Request-Id header"})
		s.logRequest(r.Method, r.URL.Path, http.StatusUnauthorized, startTime)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to read request body: %v", err))
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		s.logRequest(r.Method, r.URL.Path, http.StatusBadRequest, startTime)
		return
	}

	// Forward request to LLM provider
	llmResp, err := s.forwardToLLM(r.Context(), "/v1/completions", body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to forward request to LLM provider: %v", err))
		http.Error(w, fmt.Sprintf("Failed to forward request to LLM provider: %v", err), http.StatusInternalServerError)
		s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.Write(llmResp)
	s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
}
