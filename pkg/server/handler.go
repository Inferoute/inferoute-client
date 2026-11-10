package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/usermsg"
)

// maxRequestIDLength is larger than a real HMAC (SHA-256 base64url, ~44 bytes)
// and small enough that a flood cannot push megabyte tokens at the platform.
const maxRequestIDLength = 256

const streamWriteDeadlineGrace = 5 * time.Second

// streamWriteDeadline is when a stream must stop being written. The platform
// sends how long the stream may run (X-Inferoute-Timeout-Ms), and the deadline
// is counted from when the request arrived here. It is a duration and not a
// time of day because the platform's clock and this machine's are not the same.
func streamWriteDeadline(header string, arrived time.Time) (time.Time, bool) {
	ms, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}, false
	}
	return arrived.Add(time.Duration(ms)*time.Millisecond + streamWriteDeadlineGrace), true
}

func (s *Server) modelNotServed(w http.ResponseWriter, r *http.Request, body []byte, startTime time.Time) bool {
	if s.config == nil || s.config.Provider.Model == "" {
		return false
	}
	var payload struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Model == "" || payload.Model == s.config.Provider.Model {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "model_not_served"})
	s.logRequest(r.Method, r.URL.Path, http.StatusServiceUnavailable, startTime)
	return true
}

// handleHealth handles the /api/health endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	report, err := s.healthReporter.GetHealthReport(r.Context())
	if err != nil {
		s.logError(fmt.Sprintf("Failed to get health report: %v", err))
		http.Error(w, usermsg.HTTP(err, s.config.Provider.ProviderType), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}

// handleBusy handles the /api/busy endpoint
func (s *Server) handleBusy(w http.ResponseWriter, r *http.Request) {
	isBusy, err := s.isBusy()
	if err != nil {
		s.logError(fmt.Sprintf("Error checking if GPU is busy: %v", err))
		http.Error(w, fmt.Sprintf("Failed to check if GPU is busy: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BusyResponse{Busy: isBusy})
}

func (s *Server) writeBusy(w http.ResponseWriter, r *http.Request, startTime time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(ErrorResponse{Error: "GPU is busy"})
	s.logRequest(r.Method, r.URL.Path, http.StatusServiceUnavailable, startTime)
}

func (s *Server) writeUnauthorized(w http.ResponseWriter, r *http.Request, startTime time.Time, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
	s.logRequest(r.Method, r.URL.Path, http.StatusUnauthorized, startTime)
}

// handleChatCompletions handles the /v1/chat/completions endpoint
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleInference(w, r, "/v1/chat/completions")
}

// handleCompletions handles the /v1/completions endpoint
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleInference(w, r, "/v1/completions")
}

// handleEmbeddings handles the /v1/embeddings endpoint
func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	s.handleInference(w, r, "/v1/embeddings")
}

func (s *Server) handleInference(w http.ResponseWriter, r *http.Request, llmPath string) {
	startTime := time.Now()

	hmac := r.Header.Get("X-Request-Id")
	switch {
	case hmac == "":
		s.writeUnauthorized(w, r, startTime, "Missing HMAC in X-Request-Id header")
		return
	case len(hmac) > maxRequestIDLength:
		s.writeUnauthorized(w, r, startTime, "Invalid HMAC in X-Request-Id header")
		return
	}
	if err := s.validateHMAC(r.Context(), hmac); err != nil {
		s.logError(fmt.Sprintf("HMAC validation failed: %v", err))
		s.writeUnauthorized(w, r, startTime, fmt.Sprintf("Invalid HMAC: %v", err))
		return
	}

	// A benchmark owns the engine. Returning conversations do not skip this:
	// they skip the slot check below, and would otherwise land mid-benchmark.
	if s.benchmarking.Load() {
		s.writeBusy(w, r, startTime)
		return
	}

	// A matching X-Session-Key means this conversation was served here
	// recently. Those turns queue for a slot instead of being rejected.
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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to read request body: %v", err))
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		s.logRequest(r.Method, r.URL.Path, http.StatusBadRequest, startTime)
		return
	}

	if s.modelNotServed(w, r, body, startTime) {
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
		// Returning conversations wait for a free slot. Parallel calls that
		// share a key are admitted together, up to the slot limit.
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

	// The platform tells us how long this stream may run. Bound the upstream
	// call with that deadline too: the write deadline below never fires while
	// we are blocked reading a stalled engine, and the stream client has no
	// timeout of its own. Without this the inference slot stays taken until
	// the platform gives up.
	reqCtx := r.Context()
	writeDeadline, hasWriteDeadline := streamWriteDeadline(r.Header.Get("X-Inferoute-Timeout-Ms"), startTime)
	if hasWriteDeadline {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithDeadline(reqCtx, writeDeadline)
		defer cancel()
	}

	llmResp, err := s.forwardToLLM(reqCtx, llmPath, body)
	if err != nil {
		s.logError(fmt.Sprintf("Failed to forward request to LLM provider: %v", err))
		http.Error(w, usermsg.HTTP(err, s.config.Provider.ProviderType), http.StatusBadGateway)
		s.logRequest(r.Method, r.URL.Path, http.StatusBadGateway, startTime)
		return
	}
	defer llmResp.Body.Close()

	if ct := llmResp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	status := llmResp.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	if hasWriteDeadline {
		if err := http.NewResponseController(w).SetWriteDeadline(writeDeadline); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			s.logRequest(r.Method, r.URL.Path, http.StatusInternalServerError, startTime)
			return
		}
	}
	w.WriteHeader(status)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := llmResp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				s.logError(fmt.Sprintf("Failed to write LLM response: %v", writeErr))
				s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			s.logError(fmt.Sprintf("Failed to read LLM response: %v", readErr))
			s.logRequest(r.Method, r.URL.Path, http.StatusOK, startTime)
			return
		}
	}
	s.logRequest(r.Method, r.URL.Path, status, startTime)
}
