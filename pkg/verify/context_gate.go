package verify

import (
	"context"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

// applyContextGate fails verification when live engine context is below catalog max_model_len.
// Catalog null/zero → no gate. Ollama never reaches this (service_type=ollama).
func applyContextGate(ctx context.Context, llmClient llm.Client, model *llm.Model, entry CatalogEntry) {
	if model == nil || entry.MaxModelLen == nil || *entry.MaxModelLen <= 0 {
		return
	}
	required := *entry.MaxModelLen
	live := model.MaxModelLen
	if live <= 0 {
		// Per-request verification passes a bare model (ID only); ask the engine.
		live = readLiveMaxModelLen(ctx, llmClient, model.ID)
	}
	if live <= 0 {
		live = readFreeTokenCacheCapacity(ctx, llmClient)
	}
	if live <= 0 {
		logger.Warn("model context unreadable; failing verification against catalog max_model_len",
			zap.String("alias", model.ID),
			zap.Int64("required_max_model_len", required),
		)
		model.VerificationStatus = string(StatusFailed)
		return
	}
	if live < required {
		logger.Warn("model context below catalog max_model_len; failing verification",
			zap.String("alias", model.ID),
			zap.Int64("required_max_model_len", required),
			zap.Int64("live_max_model_len", live),
		)
		model.VerificationStatus = string(StatusFailed)
		return
	}
}

func readLiveMaxModelLen(ctx context.Context, llmClient llm.Client, modelID string) int64 {
	if llmClient == nil {
		return 0
	}
	resp, err := llmClient.ListModels(ctx)
	if err != nil || resp == nil {
		return 0
	}
	for _, m := range resp.Models {
		if m.ID == modelID {
			return m.MaxModelLen
		}
	}
	return 0
}

func readFreeTokenCacheCapacity(ctx context.Context, llmClient llm.Client) int64 {
	vc, ok := llmClient.(*llm.VLLMClient)
	if !ok {
		return 0
	}
	n, err := vc.FreeTokenCacheCapacity(ctx)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
