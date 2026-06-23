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

// Catalog caches the public approved-model catalog (no hashes).
type Catalog struct {
	baseURL     string
	serviceType string
	client      *http.Client

	mu      sync.RWMutex
	entries map[string]CatalogEntry // alias -> entry
}

// NewCatalog creates a catalog client.
func NewCatalog(baseURL, serviceType string) *Catalog {
	return &Catalog{
		baseURL:     stringsTrimRightSlash(baseURL),
		serviceType: serviceType,
		client:      &http.Client{Timeout: 30 * time.Second},
		entries:     make(map[string]CatalogEntry),
	}
}

// Refresh fetches the public catalog for this service type.
func (c *Catalog) Refresh(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/models/approved-builds?service_type=%s", c.baseURL, c.serviceType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch model catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("model catalog returned %d: %s", resp.StatusCode, string(body))
	}

	var list catalogResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return fmt.Errorf("decode model catalog: %w", err)
	}

	next := make(map[string]CatalogEntry, len(list.Data))
	for _, entry := range list.Data {
		if entry.IsActive {
			next[entry.Alias] = entry
		}
	}

	c.mu.Lock()
	c.entries = next
	c.mu.Unlock()
	return nil
}

// Get returns a catalog entry for alias, if listed.
func (c *Catalog) Get(alias string) (CatalogEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[alias]
	return entry, ok
}

// Aliases returns approved aliases in the catalog.
func (c *Catalog) Aliases() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.entries))
	for alias := range c.entries {
		out = append(out, alias)
	}
	return out
}
