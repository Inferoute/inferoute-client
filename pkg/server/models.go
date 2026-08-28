package server

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/cloudflare"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

// Server represents the HTTP server
type Server struct {
	config           *config.Config
	gpuMonitor       *gpu.Monitor
	healthReporter   *health.Reporter
	llmClient        llm.Client
	verifier         *verify.Verifier
	cloudflareClient *cloudflare.Client
	consoleUI        bool
	maxInflight      int32
	inflight         atomic.Int32
	sessionQueueWait time.Duration
	// activeSessionKey is the X-Session-Key of the in-flight (or last
	// completed) inference. Matching follow-up turns queue for the slot
	// instead of being rejected, because their KV cache is warm here.
	sessionMu        sync.Mutex
	activeSessionKey string
	server           *http.Server
	errorLog         []string
	errorLogMutex    sync.Mutex
	requestStats     struct {
		Total        int
		Success      int
		Errors       int
		Unauthorized int
		LastRequests []RequestLog
		mutex        sync.Mutex
	}
}

// BusyResponse is the response structure for the busy endpoint
type BusyResponse struct {
	Busy bool `json:"busy"`
}

// HMACValidationRequest is the request structure for HMAC validation
type HMACValidationRequest struct {
	HMAC string `json:"hmac"`
}

// HMACValidationResponse is the response structure for HMAC validation
type HMACValidationResponse struct {
	Valid bool `json:"valid"`
}

// ErrorResponse is the standard error response structure
type ErrorResponse struct {
	Error string `json:"error"`
}
