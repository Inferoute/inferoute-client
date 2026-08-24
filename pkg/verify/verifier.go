package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

type fileStat struct {
	size    int64
	modTime int64
}

type fingerprintCache struct {
	files []FileMeasurement
	stats map[string]fileStat
}

// Verifier measures local models for health reporting. It does not gate inference
// and does not call the platform verify-model API.
type Verifier struct {
	catalog           *Catalog
	serviceType       string
	hfHubCache        string
	modelPathOverride string
	clientVersion     string

	mu    sync.Mutex
	cache map[string]*fingerprintCache // alias -> weight fingerprint cache (vLLM)
}

// NewVerifier creates a model measurer. Catalog is optional (compatibility command only).
func NewVerifier(catalog *Catalog, serviceType, hfHubCache, modelPathOverride, clientVersion string) *Verifier {
	return &Verifier{
		catalog:           catalog,
		serviceType:       strings.ToLower(serviceType),
		hfHubCache:        strings.TrimSpace(hfHubCache),
		modelPathOverride: strings.TrimSpace(modelPathOverride),
		clientVersion:     clientVersion,
		cache:             make(map[string]*fingerprintCache),
	}
}

func (v *Verifier) resolveVLLMRoot(alias string) (string, error) {
	if v.modelPathOverride != "" {
		abs, err := filepath.Abs(v.modelPathOverride)
		if err != nil {
			return "", err
		}
		if dirHasWeights(abs) {
			return abs, nil
		}
	}

	hub := v.hfHubCache
	if hub == "" {
		var err error
		hub, err = DefaultHFHubCache()
		if err != nil {
			return "", err
		}
	}
	return ResolveHFModelRoot(hub, alias, "")
}

// MeasureOllamaModel copies digest/size from local tags. No server round-trip.
func (v *Verifier) MeasureOllamaModel(alias, digest string, sizeBytes int64, details map[string]interface{}) llm.Model {
	return llm.Model{
		ID:            alias,
		Object:        "model",
		OwnedBy:       "ollama",
		Digest:        NormalizeDigest(digest),
		SizeBytes:     sizeBytes,
		ServiceType:   "ollama",
		Details:       details,
		ClientVersion: v.clientVersion,
	}
}

// MeasureVLLMModel hashes local weights without requiring the approved catalog.
func (v *Verifier) MeasureVLLMModel(alias string) (llm.Model, error) {
	out := llm.Model{
		ID:            alias,
		Object:        "model",
		OwnedBy:       "vllm",
		ServiceType:   "vllm",
		HFRepo:        alias,
		ClientVersion: v.clientVersion,
	}

	root, err := v.resolveVLLMRoot(alias)
	if err != nil {
		return out, fmt.Errorf("locate weights for %s: %w", alias, err)
	}

	files, err := v.measureWithCache(alias, root)
	if err != nil {
		return out, err
	}

	var size int64
	for _, f := range files {
		size += f.Size
	}

	out.HFRevision = revisionFromSnapshotPath(root)
	out.Files = toLLMFiles(files)
	out.WeightFingerprint = AggregateFingerprint(files)
	out.SizeBytes = size
	return out, nil
}

func revisionFromSnapshotPath(root string) string {
	clean := filepath.Clean(root)
	parts := strings.Split(clean, string(filepath.Separator))
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "snapshots" {
			return parts[i+1]
		}
	}
	return ""
}

func toLLMFiles(files []FileMeasurement) []llm.FileMeasurement {
	out := make([]llm.FileMeasurement, len(files))
	for i, f := range files {
		out[i] = llm.FileMeasurement{
			Name:       f.Name,
			Hash:       f.Hash,
			HashMethod: f.HashMethod,
			Size:       f.Size,
		}
	}
	return out
}

func (v *Verifier) measureWithCache(alias, root string) (files []FileMeasurement, err error) {
	currentStats, err := weightDirStats(root)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	prev, hadCache := v.cache[alias]
	if hadCache && fileStatsEqual(prev.stats, currentStats) {
		return prev.files, nil
	}

	files, err = measureWeightDir(root)
	if err != nil {
		return nil, err
	}

	v.cache[alias] = &fingerprintCache{files: files, stats: currentStats}
	return files, nil
}

func weightDirStats(root string) (map[string]fileStat, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	stats := make(map[string]fileStat)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		stats[entry.Name()] = fileStat{size: info.Size(), modTime: info.ModTime().UnixNano()}
	}
	return stats, nil
}

func fileStatsEqual(a, b map[string]fileStat) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || va != vb {
			return false
		}
	}
	return true
}

// MeasureModels enriches discovered models with local measurement fields.
// Catalog / verify-model server are not required.
func (v *Verifier) MeasureModels(ctx context.Context, llmClient llm.Client, models []llm.Model) []llm.Model {
	var ollamaDetails map[string]ollamaDetail
	if v.serviceType == "ollama" {
		if oc, ok := llmClient.(*llm.OllamaClient); ok {
			if tags, err := oc.ListTags(ctx); err == nil {
				ollamaDetails = OllamaDetailsFromTags(tags)
			}
		}
	}

	out := make([]llm.Model, len(models))
	for i, m := range models {
		out[i] = m
		out[i].ClientVersion = v.clientVersion
		switch v.serviceType {
		case "ollama":
			detail, ok := ollamaDetails[m.ID]
			if !ok {
				out[i].ServiceType = "ollama"
				if out[i].Digest != "" {
					out[i].Digest = NormalizeDigest(out[i].Digest)
				}
				continue
			}
			measured := v.MeasureOllamaModel(m.ID, detail.Digest, detail.Size, detail.Details)
			out[i].Digest = measured.Digest
			out[i].SizeBytes = measured.SizeBytes
			out[i].ServiceType = measured.ServiceType
			out[i].Details = measured.Details
			out[i].ClientVersion = measured.ClientVersion
		case "vllm":
			measured, err := v.MeasureVLLMModel(m.ID)
			if err != nil {
				logger.Error("vLLM measurement error", zap.String("alias", m.ID), zap.Error(err))
				out[i].ServiceType = "vllm"
				continue
			}
			out[i].ServiceType = measured.ServiceType
			out[i].HFRepo = measured.HFRepo
			out[i].HFRevision = measured.HFRevision
			out[i].Files = measured.Files
			out[i].WeightFingerprint = measured.WeightFingerprint
			out[i].SizeBytes = measured.SizeBytes
			out[i].ClientVersion = measured.ClientVersion
		default:
			out[i].ServiceType = v.serviceType
		}
	}
	return out
}

// ApplyToModels is retained as an alias for MeasureModels.
func (v *Verifier) ApplyToModels(ctx context.Context, llmClient llm.Client, models []llm.Model) []llm.Model {
	return v.MeasureModels(ctx, llmClient, models)
}

type ollamaDetail struct {
	Digest  string
	Size    int64
	Details map[string]interface{}
}

// OllamaDetailsFromTags maps Ollama tag entries to consumer aliases (gguf/...).
func OllamaDetailsFromTags(tags []llm.OllamaModel) map[string]ollamaDetail {
	out := make(map[string]ollamaDetail, len(tags))
	for _, t := range tags {
		format := "unknown"
		if details, ok := t.Details["format"]; ok {
			format = fmt.Sprintf("%v", details)
		}
		alias := fmt.Sprintf("%s/%s", format, t.Model)
		out[alias] = ollamaDetail{Digest: t.Digest, Size: t.Size, Details: t.Details}
	}
	return out
}

// CatalogClient exposes the public model catalog.
func (v *Verifier) CatalogClient() *Catalog {
	return v.catalog
}

// RefreshCatalog reloads the public approved-model catalog (compatibility command).
func (v *Verifier) RefreshCatalog(ctx context.Context) error {
	if v.catalog == nil {
		return nil
	}
	return v.catalog.Refresh(ctx)
}
