package verify

// CatalogEntry is a public approved-model row (no verification secrets).
// Used by the compatibility command; measuring does not require the catalog.
type CatalogEntry struct {
	ID                    string   `json:"id"`
	Alias                 string   `json:"alias"`
	ServiceType           string   `json:"service_type"`
	DisplayName           string   `json:"display_name"`
	Description           *string  `json:"description,omitempty"`
	CardImage             string   `json:"card_image"`
	HFRepo                *string  `json:"hf_repo,omitempty"`
	HFRef                 *string  `json:"hf_ref,omitempty"`
	MinSizeBytes          int64    `json:"min_size_bytes"`
	IsActive              bool     `json:"is_active"`
	InputPricePer1M       *float64 `json:"input_price_per_1m,omitempty"`
	OutputPricePer1M      *float64 `json:"output_price_per_1m,omitempty"`
	TransactionCount      int64    `json:"transaction_count"`
	TotalProviderEarnings float64  `json:"total_provider_earnings"`
	SortOrder             *int32   `json:"sort_order,omitempty"`
}

// catalogResponse is the public list from GET /api/models/approved-builds.
type catalogResponse struct {
	Object string         `json:"object"`
	Data   []CatalogEntry `json:"data"`
}

// FileMeasurement is one hashed weight file.
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
