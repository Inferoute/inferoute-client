package health

import "context"

// ModelEntry is one model row for console or API display.
type ModelEntry struct {
	ID                 string
	VerificationStatus string
}

// ModelDisplay is a snapshot of local model status for the UI.
type ModelDisplay struct {
	Models []ModelEntry
	Err    error
}

// ModelDisplay polls the local LLM and returns models with verification status.
func (r *Reporter) ModelDisplay(ctx context.Context) ModelDisplay {
	models, err := r.RefreshModelsForDisplay(ctx)
	if err != nil {
		return ModelDisplay{Err: err}
	}

	entries := make([]ModelEntry, len(models))
	for i, m := range models {
		entries[i] = ModelEntry{
			ID:                 m.ID,
			VerificationStatus: m.VerificationStatus,
		}
	}
	return ModelDisplay{Models: entries}
}
