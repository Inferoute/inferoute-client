package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/inferscale"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/usermsg"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
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
	Connected        bool          `json:"connected"`
	Models           []StatusModel `json:"models"`
	GPU              *gpu.GPUInfo  `json:"gpu"`
	RecentRequests   []RequestLog  `json:"recent_requests"`
	Errors           []string      `json:"errors"`
	ClientVersion    string        `json:"client_version"`
	LatestVersion    string        `json:"latest_version,omitempty"`
	UpdateStatus     string        `json:"update_status"`
	TailnetIP        string        `json:"tailnet_ip,omitempty"`
	Peers            string        `json:"peers,omitempty"`
	Cluster          string        `json:"cluster,omitempty"`
	Nodes            string        `json:"nodes,omitempty"`
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

	upd := nodeapi.State()
	tailnetIP, online := inferscale.Tailnet()
	peers := ""
	if online > 0 {
		peers = fmt.Sprintf("%d online", online)
	}
	cluster := "single"
	nodes := ""
	if s.config != nil {
		switch s.config.Provider.ClusterMode {
		case "master", "worker":
			cluster = s.config.Provider.ClusterMode
		}
		if joined, need := engine.NodeSlots(tailnetIP != "", online, engine.NNodes(s.config.Provider.EngineArgs)); need > 0 {
			nodes = fmt.Sprintf("%d/%d", joined, need)
			if cluster == "single" {
				cluster = "master"
			}
		}
	}
	return StatusSnapshot{
		LastHealthUpdate: lastUpdateStr,
		SessionStatus:    "online",
		ProviderType:     providerType,
		ProviderAPIKey:   apiKey,
		ProviderURL:      providerURL,
		LLMURL:           llmURL,
		Connected:        s.gatewayConnected(),
		Models:           models,
		GPU:              gpuInfo,
		RecentRequests:   reqs,
		Errors:           errs,
		ClientVersion:    version.Version,
		LatestVersion:    upd.Latest,
		UpdateStatus:     string(upd.Status),
		TailnetIP:        tailnetIP,
		Peers:            peers,
		Cluster:          cluster,
		Nodes:            nodes,
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
