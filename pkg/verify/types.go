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

// CatalogEntry is a public approved-model row (no verification secrets).
type CatalogEntry struct {
	ID          string  `json:"id"`
	Alias       string  `json:"alias"`
	ServiceType string  `json:"service_type"`
	HFRepo      *string `json:"hf_repo,omitempty"`
	HFRef       *string `json:"hf_ref,omitempty"`
	IsActive    bool    `json:"is_active"`
}

// catalogResponse is the public list from GET /api/models/approved-builds.
type catalogResponse struct {
	Object string         `json:"object"`
	Data   []CatalogEntry `json:"data"`
}

// FileMeasurement is one hashed weight file sent to the server for verification.
type FileMeasurement struct {
	Name       string `json:"name"`
	Hash       string `json:"hash"`
	HashMethod string `json:"hash_method"`
	Size       int64  `json:"size"`
}

// ManifestEntry is used by local fingerprint helpers and tests (not returned by the public API).
type ManifestEntry struct {
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	HashMethod string `json:"hash_method"`
	Size       int64  `json:"size"`
}

// verifyModelRequest is POST /api/provider/verify-model.
type verifyModelRequest struct {
	Alias  string            `json:"alias"`
	Digest string            `json:"digest,omitempty"`
	SizeBytes int64          `json:"size_bytes,omitempty"`
	Files  []FileMeasurement `json:"files,omitempty"`
	Stale  bool              `json:"stale,omitempty"`
}

// verifyModelResponse is the server-as-judge verification result.
type verifyModelResponse struct {
	VerificationStatus string `json:"verification_status"`
	Digest             string `json:"digest,omitempty"`
	WeightFingerprint  string `json:"weight_fingerprint,omitempty"`
}

// Result holds verification output for one model alias.
type Result struct {
	Alias             string
	Status            Status
	Digest            string
	WeightFingerprint string
	SizeBytes         int64
}
