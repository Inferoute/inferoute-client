package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/usermsg"
)

//go:embed dashboard.html
var dashboardHTML []byte

// RequestLog is one recent HTTP request for the console and dashboard.
type RequestLog struct {
	Time     string `json:"time"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	Duration string `json:"duration"`
}

// StatusModel is one model row on the status dashboard.
type StatusModel struct {
	ID                  string `json:"id"`
	MarketplaceApproval string `json:"marketplace_approval"`
	VerificationStatus  string `json:"verification_status"`
}

// StatusSnapshot is the local console view, as JSON for the browser dashboard.
type StatusSnapshot struct {
	LastHealthUpdate string        `json:"last_health_update"`
	SessionStatus    string        `json:"session_status"`
	ProviderType     string        `json:"provider_type"`
	ProviderAPIKey   string        `json:"provider_api_key"`
	ProviderURL      string        `json:"provider_url"`
	LLMURL           string        `json:"llm_url"`
	TunnelURL        string        `json:"cloudflare_tunnel_url,omitempty"`
	Models           []StatusModel `json:"models"`
	GPU              *gpu.GPUInfo  `json:"gpu"`
	RecentRequests   []RequestLog  `json:"recent_requests"`
	Errors           []string      `json:"errors"`
}

func (s *Server) snapshot() StatusSnapshot {
	gpuInfo := &gpu.GPUInfo{
		ProductName:   "Unknown",
		DriverVersion: "Unknown",
		CUDAVersion:   "Unknown",
		GPUCount:      0,
	}
	if s.gpuMonitor != nil {
		if info, err := s.gpuMonitor.GetGPUInfo(); err == nil && info != nil {
			gpuInfo = info
		}
	}

	var tunnelURL string
	if s.cloudflareClient != nil {
		tunnelURL = s.cloudflareClient.GetTunnelURL()
	}

	lastUpdateStr := "Never"
	var models []StatusModel
	if s.healthReporter != nil {
		if lastUpdate := s.healthReporter.GetLastUpdateTime(); !lastUpdate.IsZero() {
			lastUpdateStr = lastUpdate.Format("2006-01-02 15:04:05")
		}
		for _, m := range s.healthReporter.GetDisplayedModels() {
			models = append(models, StatusModel{
				ID:                  m.ID,
				MarketplaceApproval: usermsg.ApprovalLabel(m.VerificationStatus),
				VerificationStatus:  m.VerificationStatus,
			})
		}
	}

	s.requestStats.mutex.Lock()
	reqs := make([]RequestLog, len(s.requestStats.LastRequests))
	copy(reqs, s.requestStats.LastRequests)
	s.requestStats.mutex.Unlock()

	s.errorLogMutex.Lock()
	errs := make([]string, len(s.errorLog))
	copy(errs, s.errorLog)
	s.errorLogMutex.Unlock()

	providerType := ""
	apiKey := ""
	providerURL := ""
	llmURL := ""
	if s.config != nil {
		providerType = s.config.Provider.ProviderType
		apiKey = maskStringHelper(s.config.Provider.APIKey)
		providerURL = s.config.Provider.URL
		llmURL = s.config.Provider.LLMURL
	}

	return StatusSnapshot{
		LastHealthUpdate: lastUpdateStr,
		SessionStatus:    "online",
		ProviderType:     providerType,
		ProviderAPIKey:   apiKey,
		ProviderURL:      providerURL,
		LLMURL:           llmURL,
		TunnelURL:        tunnelURL,
		Models:           models,
		GPU:              gpuInfo,
		RecentRequests:   reqs,
		Errors:           errs,
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(dashboardHTML)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.snapshot())
}

func formatDuration(d time.Duration) string {
	if d.Seconds() >= 1 {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
}
