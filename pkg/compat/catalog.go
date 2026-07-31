package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

const defaultCatalogURL = "https://core.inferoute.com"

// FetchCatalog loads approved builds from the public Inferoute catalog.
// When providerType is empty, both ollama and vllm catalogs are fetched and merged.
func FetchCatalog(ctx context.Context, catalogURL, providerType string) ([]verify.CatalogEntry, error) {
	base := strings.TrimRight(catalogURL, "/")
	if base == "" {
		base = defaultCatalogURL
	}

	types := []string{providerType}
	if strings.TrimSpace(providerType) == "" {
		types = []string{"ollama", "vllm"}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var all []verify.CatalogEntry
	seen := map[string]struct{}{}

	for _, st := range types {
		entries, err := fetchCatalogOnce(ctx, client, base, st)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			key := e.Alias + "|" + e.ServiceType
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			all = append(all, e)
		}
	}
	return all, nil
}

func fetchCatalogOnce(ctx context.Context, client *http.Client, base, serviceType string) ([]verify.CatalogEntry, error) {
	url := fmt.Sprintf("%s/api/models/approved-builds", base)
	if serviceType != "" {
		url += "?service_type=" + serviceType
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("catalog returned %d: %s", resp.StatusCode, string(body))
	}

	var list struct {
		Object string                `json:"object"`
		Data   []verify.CatalogEntry `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}

	out := make([]verify.CatalogEntry, 0, len(list.Data))
	for _, e := range list.Data {
		if e.IsActive {
			out = append(out, e)
		}
	}
	return out, nil
}

// LoadOfflineCatalog reads a local approved-builds JSON file (same shape as the public API).
func LoadOfflineCatalog(path, providerType string) ([]verify.CatalogEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list struct {
		Object string                `json:"object"`
		Data   []verify.CatalogEntry `json:"data"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		// Also accept a bare array.
		var arr []verify.CatalogEntry
		if err2 := json.Unmarshal(raw, &arr); err2 != nil {
			return nil, fmt.Errorf("decode offline catalog: %w", err)
		}
		list.Data = arr
	}

	want := strings.ToLower(strings.TrimSpace(providerType))
	out := make([]verify.CatalogEntry, 0, len(list.Data))
	for _, e := range list.Data {
		if !e.IsActive {
			continue
		}
		if want != "" && strings.ToLower(e.ServiceType) != want {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
