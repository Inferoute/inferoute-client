package health

import "context"

// ModelEntry is one model row for console or API display.
type ModelEntry struct {
	ID                string
	Digest            string
	SizeBytes         int64
	Files             int
	WeightFingerprint string
	ServiceType       string
}

// ModelDisplay is a snapshot of local model status for the UI.
type ModelDisplay struct {
	Models []ModelEntry
	Err    error
}

// ModelDisplay polls the local LLM and returns models with measurement fields.
func (r *Reporter) ModelDisplay(ctx context.Context) ModelDisplay {
	models, err := r.RefreshModelsForDisplay(ctx)
	if err != nil {
		return ModelDisplay{Err: err}
	}

	entries := make([]ModelEntry, len(models))
	for i, m := range models {
		entries[i] = ModelEntry{
			ID:                m.ID,
			Digest:            m.Digest,
			SizeBytes:         m.SizeBytes,
			Files:             len(m.Files),
			WeightFingerprint: m.WeightFingerprint,
			ServiceType:       m.ServiceType,
		}
	}
	return ModelDisplay{Models: entries}
}
