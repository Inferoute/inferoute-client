package verify

// Status is the model integrity verification outcome reported to the platform.
type Status string

const (
	StatusVerified   Status = "verified"
	StatusPending    Status = "pending"
	StatusStale      Status = "stale"
	StatusFailed     Status = "failed"
	StatusUnverified Status = "unverified"
)

// ManifestEntry is one file in a vLLM approved build manifest.
type ManifestEntry struct {
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	HashMethod string `json:"hash_method"`
	Size       int64  `json:"size"`
}

// ApprovedBuild is a platform-approved model artifact.
type ApprovedBuild struct {
	ID                string          `json:"id"`
	Alias             string          `json:"alias"`
	ServiceType       string          `json:"service_type"`
	ExpectedDigest    *string         `json:"expected_digest,omitempty"`
	WeightFingerprint *string         `json:"weight_fingerprint,omitempty"`
	MinSizeBytes      int64           `json:"min_size_bytes"`
	HFRepo            *string         `json:"hf_repo,omitempty"`
	HFRevision        *string         `json:"hf_revision,omitempty"`
	Manifest          []ManifestEntry `json:"manifest,omitempty"`
	IsActive          bool            `json:"is_active"`
}

// approvedBuildsResponse is the public list response from GET /api/models/approved-builds.
type approvedBuildsResponse struct {
	Object string          `json:"object"`
	Data   []ApprovedBuild `json:"data"`
}

// Result holds verification output for one model alias.
type Result struct {
	Alias              string
	Status             Status
	Digest             string
	WeightFingerprint  string
	SizeBytes          int64
	ApprovedBuildID    string
}
