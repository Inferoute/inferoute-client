package server

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"context"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/connect"
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
	connector        *connect.Client
	connectCancel    context.CancelFunc
	consoleUI        bool
	maxInflight      atomic.Int32
	inflight         atomic.Int32
	benchmarking     atomic.Bool
	onCapacity       func()
	engineNote       string
	engineNoteMu     sync.Mutex
	sessionQueueWait time.Duration
	// sessions remembers recent X-Session-Key values so a returning
	// conversation can wait for a slot. There is no per-session lock:
	// parallel agent calls often share one key, and serialising them
	// would throw away the batching the slot limit exists for.
	sessionMu    sync.Mutex
	sessions     map[string]time.Time
	server       *http.Server
	requestStats struct {
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
