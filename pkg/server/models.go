package server

import (
	"net/http"
	"sync"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/cloudflare"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
)

// Server represents the HTTP server
type Server struct {
	config           *config.Config
	gpuMonitor       *gpu.Monitor
	healthReporter   *health.Reporter
	llmClient        llm.Client
	cloudflareClient *cloudflare.Client
	server           *http.Server
	errorLog         []string
	errorLogMutex    sync.Mutex
	requestStats     struct {
		Total        int
		Success      int
		Errors       int
		Unauthorized int
		LastRequests []string
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
