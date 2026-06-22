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
	fingerprint string
	files       map[string]fileStat
}

// Verifier checks local models against the platform allowlist.
type Verifier struct {
	registry          *Registry
	serviceType       string
	hfHubCache        string // optional override; empty = ~/.cache/huggingface/hub
	modelPathOverride string // optional flat weight dir (hf download --local-dir)

	mu    sync.Mutex
	cache map[string]*fingerprintCache // alias -> cache
}

// NewVerifier creates a verifier. For vLLM, weights are auto-resolved from the
// HuggingFace hub cache using the served model id and approved hf_revision.
// hfHubCache and modelPathOverride are optional.
func NewVerifier(registry *Registry, serviceType, hfHubCache, modelPathOverride string) *Verifier {
	return &Verifier{
		registry:          registry,
		serviceType:       strings.ToLower(serviceType),
		hfHubCache:        strings.TrimSpace(hfHubCache),
		modelPathOverride: strings.TrimSpace(modelPathOverride),
		cache:             make(map[string]*fingerprintCache),
	}
}

func (v *Verifier) resolveVLLMRoot(build ApprovedBuild, alias string) (string, error) {
	if v.modelPathOverride != "" {
		abs, err := filepath.Abs(v.modelPathOverride)
		if err != nil {
			return "", err
		}
		if dirHasWeights(abs) {
			return abs, nil
		}
	}

	repo := hfRepoForBuild(alias, build)
	revision := hfRevisionForBuild(build)
	hub := v.hfHubCache
	if hub == "" {
		var err error
		hub, err = DefaultHFHubCache()
		if err != nil {
			return "", err
		}
	}
	return ResolveHFModelRoot(hub, repo, revision)
}

// VerifyOllamaModel checks digest and size from Ollama /api/tags data.
func (v *Verifier) VerifyOllamaModel(alias, digest string, sizeBytes int64) Result {
	res := Result{Alias: alias, Digest: NormalizeDigest(digest), SizeBytes: sizeBytes}

	build, ok := v.registry.Get(alias)
	if !ok {
		res.Status = StatusUnverified
		return res
	}
	res.ApprovedBuildID = build.ID

	if build.ExpectedDigest == nil {
		res.Status = StatusFailed
		return res
	}
	expected := NormalizeDigest(*build.ExpectedDigest)
	if res.Digest == "" {
		res.Status = StatusFailed
		return res
	}
	if res.Digest != expected {
		logger.Warn("Ollama digest mismatch",
			zap.String("alias", alias),
			zap.String("expected", expected),
			zap.String("actual", res.Digest))
		res.Status = StatusFailed
		return res
	}
	if sizeBytes > 0 && sizeBytes < build.MinSizeBytes {
		logger.Warn("Ollama model below minimum size",
			zap.String("alias", alias),
			zap.Int64("min_size_bytes", build.MinSizeBytes),
			zap.Int64("actual_size_bytes", sizeBytes))
		res.Status = StatusFailed
		return res
	}
	res.Status = StatusVerified
	return res
}

// VerifyVLLMModel fingerprints model_path against the approved manifest.
func (v *Verifier) VerifyVLLMModel(ctx context.Context, alias string) (Result, error) {
	res := Result{Alias: alias}

	build, ok := v.registry.Get(alias)
	if !ok {
		res.Status = StatusUnverified
		return res, nil
	}
	res.ApprovedBuildID = build.ID

	if build.WeightFingerprint == nil || len(build.Manifest) == 0 {
		res.Status = StatusFailed
		return res, nil
	}

	root, err := v.resolveVLLMRoot(build, alias)
	if err != nil {
		res.Status = StatusFailed
		return res, fmt.Errorf("locate weights for %s: %w", alias, err)
	}

	fingerprint, stale, err := v.fingerprintWithCache(alias, root, build.Manifest)
	if err != nil {
		res.Status = StatusFailed
		return res, err
	}
	res.WeightFingerprint = fingerprint

	expected := strings.ToLower(strings.TrimSpace(*build.WeightFingerprint))
	if fingerprint != expected {
		logger.Warn("vLLM fingerprint mismatch",
			zap.String("alias", alias),
			zap.String("expected", expected),
			zap.String("actual", fingerprint))
		res.Status = StatusFailed
		return res, nil
	}

	if stale {
		res.Status = StatusStale
	} else {
		res.Status = StatusVerified
	}
	return res, nil
}

func (v *Verifier) fingerprintWithCache(alias, root string, manifest []ManifestEntry) (fingerprint string, stale bool, err error) {
	currentStats, err := manifestFileStats(root, manifest)
	if err != nil {
		return "", false, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	prev, hadCache := v.cache[alias]
	if hadCache && fileStatsEqual(prev.files, currentStats) {
		return prev.fingerprint, false, nil
	}

	fp, err := WeightFingerprint(root, manifest)
	if err != nil {
		return "", false, err
	}

	v.cache[alias] = &fingerprintCache{fingerprint: fp, files: currentStats}
	return fp, hadCache, nil
}

func manifestFileStats(root string, manifest []ManifestEntry) (map[string]fileStat, error) {
	stats := make(map[string]fileStat, len(manifest))
	for _, entry := range manifest {
		path := filepath.Join(root, entry.Name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", entry.Name, err)
		}
		stats[entry.Name] = fileStat{size: info.Size(), modTime: info.ModTime().UnixNano()}
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

// ApplyToModels enriches discovered models with verification fields.
func (v *Verifier) ApplyToModels(ctx context.Context, llmClient llm.Client, models []llm.Model) []llm.Model {
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
		switch v.serviceType {
		case "ollama":
			detail, ok := ollamaDetails[m.ID]
			if !ok {
				out[i].VerificationStatus = string(StatusUnverified)
				continue
			}
			res := v.VerifyOllamaModel(m.ID, detail.Digest, detail.Size)
			applyResult(&out[i], res)
		case "vllm":
			res, err := v.VerifyVLLMModel(ctx, m.ID)
			if err != nil {
				logger.Error("vLLM verification error", zap.String("alias", m.ID), zap.Error(err))
				out[i].VerificationStatus = string(StatusFailed)
				continue
			}
			applyResult(&out[i], res)
		default:
			out[i].VerificationStatus = string(StatusUnverified)
		}
	}
	return out
}

func applyResult(m *llm.Model, res Result) {
	m.VerificationStatus = string(res.Status)
	m.Digest = res.Digest
	m.WeightFingerprint = res.WeightFingerprint
	m.SizeBytes = res.SizeBytes
}

// IsInferenceAllowed returns true when the model may serve traffic.
func IsInferenceAllowed(status string) bool {
	return status == string(StatusVerified)
}

// ollamaDetail holds raw /api/tags fields keyed by consumer alias.
type ollamaDetail struct {
	Digest string
	Size   int64
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
		out[alias] = ollamaDetail{Digest: t.Digest, Size: t.Size}
	}
	return out
}

// Registry exposes the approved-builds cache for periodic refresh.
func (v *Verifier) Registry() *Registry {
	return v.registry
}

// RefreshApprovedBuilds reloads the platform allowlist.
func (v *Verifier) RefreshApprovedBuilds(ctx context.Context) error {
	if v.registry == nil {
		return nil
	}
	return v.registry.Refresh(ctx)
}

func (v *Verifier) CheckInference(ctx context.Context, llmClient llm.Client, modelName string) error {
	models := v.ApplyToModels(ctx, llmClient, []llm.Model{{ID: modelName, Object: "model", OwnedBy: v.serviceType}})
	if len(models) == 0 {
		return fmt.Errorf("model %s not found", modelName)
	}
	status := models[0].VerificationStatus
	if status == "" {
		status = string(StatusUnverified)
	}
	if !IsInferenceAllowed(status) {
		return fmt.Errorf("model %s is not verified (%s)", modelName, status)
	}
	return nil
}

func VerifiedModelIDs(models []llm.Model) []string {
	var ids []string
	for _, m := range models {
		if m.VerificationStatus == string(StatusVerified) {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
