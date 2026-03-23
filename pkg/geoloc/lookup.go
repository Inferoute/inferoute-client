// Package geoloc resolves the host's public IP and a human-readable location label via HTTPS.
// Used while the Cloudflare tunnel is up so egress is likely available; failures are non-fatal for callers.
package geoloc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultLookupURL is the default HTTPS endpoint returning JSON with ip, city, region, country_name.
// Override with env INFEROUTE_GEO_LOOKUP_URL (full URL to a JSON document).
const DefaultLookupURL = "https://ipapi.co/json/"

// ipapiResponse matches the subset of https://ipapi.co/json/ we need.
type ipapiResponse struct {
	IP          string `json:"ip"`
	City        string `json:"city"`
	Region      string `json:"region"`
	CountryName string `json:"country_name"`
	Error       bool   `json:"error"`
	Reason      string `json:"reason"`
}

// Lookup performs one geolocation request and returns public IP and a display string like "City, Region, Country".
func Lookup(ctx context.Context, client *http.Client) (publicIP, displayLocation string, err error) {
	if client == nil {
		client = http.DefaultClient
	}

	url := os.Getenv("INFEROUTE_GEO_LOOKUP_URL")
	if url == "" {
		url = DefaultLookupURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("geoloc request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("geoloc http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("geoloc http status %d", resp.StatusCode)
	}

	var body ipapiResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("geoloc json: %w", err)
	}

	if body.Error {
		return "", "", fmt.Errorf("geoloc provider: %s", body.Reason)
	}

	publicIP = strings.TrimSpace(body.IP)
	parts := make([]string, 0, 3)
	for _, s := range []string{strings.TrimSpace(body.City), strings.TrimSpace(body.Region), strings.TrimSpace(body.CountryName)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	displayLocation = strings.Join(parts, ", ")
	if publicIP == "" && displayLocation == "" {
		return "", "", fmt.Errorf("geoloc empty result")
	}
	return publicIP, displayLocation, nil
}

// Client returns an HTTP client suitable for geolocation (short timeout).
func Client() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}
