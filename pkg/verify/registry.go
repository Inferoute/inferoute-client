package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Registry caches platform-approved builds from GET /api/models/approved-builds.
type Registry struct {
	baseURL    string
	serviceType string
	client     *http.Client

	mu     sync.RWMutex
	builds map[string]ApprovedBuild // alias -> build
}

// NewRegistry creates a registry client. serviceType is ollama or vllm.
func NewRegistry(baseURL, serviceType string) *Registry {
	return &Registry{
		baseURL:     stringsTrimRightSlash(baseURL),
		serviceType: serviceType,
		client:      &http.Client{Timeout: 30 * time.Second},
		builds:      make(map[string]ApprovedBuild),
	}
}

func stringsTrimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// Refresh fetches active approved builds for this service type.
func (r *Registry) Refresh(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/models/approved-builds?service_type=%s", r.baseURL, r.serviceType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch approved builds: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("approved builds returned %d: %s", resp.StatusCode, string(body))
	}

	var list approvedBuildsResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return fmt.Errorf("decode approved builds: %w", err)
	}

	next := make(map[string]ApprovedBuild, len(list.Data))
	for _, b := range list.Data {
		if b.IsActive {
			next[b.Alias] = b
		}
	}

	r.mu.Lock()
	r.builds = next
	r.mu.Unlock()
	return nil
}

// Get returns the approved build for alias, if any.
func (r *Registry) Get(alias string) (ApprovedBuild, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.builds[alias]
	return b, ok
}

// Aliases returns all approved aliases currently cached.
func (r *Registry) Aliases() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.builds))
	for alias := range r.builds {
		out = append(out, alias)
	}
	return out
}
