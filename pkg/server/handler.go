package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/usermsg"
)

// handleHealth handles the /api/health endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Get health report
	report, err := s.healthReporter.GetHealthReport(r.Context())
	if err != nil {
		s.logError(fmt.Sprintf("Failed to get health report: %v", err))
		http.Error(w, usermsg.HTTP(err, s.config.Provider.ProviderType), http.StatusInternalServerError)
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

	isBusy, err := s.isBusy()
	if err != nil {
		s.logError(fmt.Sprintf("Error checking if GPU is busy: %v", err))
		http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
		s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BusyResponse{Busy: isBusy})
	s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
}

func (s *Server) writeBusy(w http.ResponseWriter, r *http.Request, startTime time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(ErrorResponse{Error: "GPU is busy"})
	s.logRequest(r.Method, r.URL.Path, http.StatusServiceUnavailable, startTime)
}

// handleChatCompletions handles the /v1/chat/completions endpoint
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleInference(w, r, "/v1/chat/completions")
}

// handleCompletions handles the /v1/completions endpoint
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleInference(w, r, "/v1/completions")
}

func (s *Server) handleInference(w http.ResponseWriter, r *http.Request, llmPath string) {
	startTime := time.Now()

	// A matching X-Session-Key means this is a follow-up turn of the
	// conversation whose KV cache is warm on this GPU. Those turns skip the
	// busy rejection (the GPU-util arm fires exactly while the previous turn
	// of the same session is decoding) and queue for the slot instead.
	sessionKey := r.Header.Get("X-Session-Key")
	sameSession := sessionKey != "" && s.isActiveSession(sessionKey)

	if !sameSession {
		isBusy, err := s.isBusy()
		if err != nil {
			s.logError(fmt.Sprintf("Error checking if GPU is busy: %v", err))
			http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
			s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
			return
		}
		if isBusy {
			s.writeBusy(w, r, startTime)
			return
		}
	}

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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to read request body: %v", err))
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		s.logRequest(r.Method, r.URL.Path, http.StatusBadRequest, startTime)
		return
	}

	if err := s.verifyModelInRequest(r.Context(), body); err != nil {
		s.logError(fmt.Sprintf("Model verification failed: %v", err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		s.logRequest(r.Method, r.URL.Path, http.StatusForbidden, startTime)
		return
	}

	acquired := false
	if sameSession {
		// Same-session turns are serial, so this queue has depth 1 in
		// practice; the bounded wait covers the previous turn's decode.
		acquired = s.acquireInferenceWait(r.Context(), s.sessionQueueWait)
	} else {
		acquired = s.tryAcquireInference()
	}
	if !acquired {
		s.writeBusy(w, r, startTime)
		return
	}
	defer s.releaseInference()
	s.setActiveSession(sessionKey)

	llmResp, err := s.forwardToLLM(r.Context(), llmPath, body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to forward request to LLM provider: %v", err))
		http.Error(w, usermsg.HTTP(err, s.config.Provider.ProviderType), http.StatusBadGateway)
		s.logRequest(r.Method, r.URL.Path, http.StatusBadGateway, startTime)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(llmResp)
	s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
}
