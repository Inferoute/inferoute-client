package nodeapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
)

const (
	HeaderClientVersion       = "X-Inferoute-Client-Version"
	HeaderLatestClientVersion = "X-Inferoute-Latest-Client-Version"

	// UpgradeRetryInterval is how long the health loop waits after a 426
	// before trying again.
	UpgradeRetryInterval = 30 * time.Minute
)

// UpdateStatus is what the running client should tell the operator.
type UpdateStatus string

const (
	UpdateCurrent   UpdateStatus = "current"
	UpdateAvailable UpdateStatus = "available"
	UpdateRequired  UpdateStatus = "required"
)

// ErrUpgradeRequired is returned when the node answers 426.
type ErrUpgradeRequired struct {
	Min         string `json:"min_version"`
	Latest      string `json:"latest_version"`
	DownloadURL string `json:"download_url"`
}

func (e *ErrUpgradeRequired) Error() string {
	return fmt.Sprintf("client update required: running %s, minimum %s, latest %s", version.Version, e.Min, e.Latest)
}

type upgradeBody struct {
	Error       string `json:"error"`
	Min         string `json:"min_version"`
	Latest      string `json:"latest_version"`
	DownloadURL string `json:"download_url"`
}

// Snapshot is a copy of the latest update signal seen on node responses.
type Snapshot struct {
	Status      UpdateStatus
	Latest      string
	Min         string
	DownloadURL string
}

var (
	stateMu sync.Mutex
	state   = Snapshot{Status: UpdateCurrent}
)

// State returns the latest update signal observed from node responses.
func State() Snapshot {
	stateMu.Lock()
	defer stateMu.Unlock()
	return state
}

// NoteLatest records a successful response advertising the newest client release.
// A 200 means this binary is still accepted, so a previous "required" state clears.
func NoteLatest(latest string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state.Latest = latest
	state.Min = ""
	state.DownloadURL = ""
	if Newer(latest, version.Version) {
		state.Status = UpdateAvailable
		return
	}
	state.Status = UpdateCurrent
}

// NoteRequired records a 426 and keeps it until a later success.
func NoteRequired(min, latest, downloadURL string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	state.Status = UpdateRequired
	state.Min = min
	if latest != "" {
		state.Latest = latest
	}
	state.DownloadURL = downloadURL
}

// ParseUpgrade builds ErrUpgradeRequired from a 426 body and records it.
// A non-426 status returns nil. Callers that already consumed the body pass it here.
func ParseUpgrade(status int, body []byte) *ErrUpgradeRequired {
	if status != http.StatusUpgradeRequired {
		return nil
	}
	var parsed upgradeBody
	_ = json.Unmarshal(body, &parsed)
	err := &ErrUpgradeRequired{
		Min:         parsed.Min,
		Latest:      parsed.Latest,
		DownloadURL: parsed.DownloadURL,
	}
	NoteRequired(err.Min, err.Latest, err.DownloadURL)
	return err
}

// HealthInterval is the next health-report wait. A 426 slows the loop;
// any other outcome (including success) uses normal.
func HealthInterval(err error, normal time.Duration) time.Duration {
	if IsUpgradeRequired(err) {
		return UpgradeRetryInterval
	}
	return normal
}

// IsUpgradeRequired reports whether err came from a 426.
func IsUpgradeRequired(err error) bool {
	var up *ErrUpgradeRequired
	return errors.As(err, &up)
}

// NewHTTPClient is the client used for every call to the Inferoute node.
// It stamps the version headers and records update signals from responses.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &versionTransport{base: http.DefaultTransport},
	}
}

type versionTransport struct {
	base http.RoundTripper
}

func (t *versionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", version.UserAgent())
	clone.Header.Set(HeaderClientVersion, version.Version)

	resp, err := base.RoundTrip(clone)
	if err != nil || resp == nil {
		return resp, err
	}
	if latest := resp.Header.Get(HeaderLatestClientVersion); latest != "" && resp.StatusCode != http.StatusUpgradeRequired {
		NoteLatest(latest)
	}
	if resp.StatusCode == http.StatusUpgradeRequired {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		if readErr != nil {
			resp.Body = io.NopCloser(bytes.NewReader(nil))
			NoteRequired("", "", "")
			return resp, nil
		}
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		ParseUpgrade(resp.StatusCode, raw)
	}
	return resp, nil
}
