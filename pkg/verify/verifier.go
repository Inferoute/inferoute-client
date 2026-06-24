package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

type verifyResultEntry struct {
	result   Result
	cachedAt time.Time
	digest   string
	size     int64
	stats    map[string]fileStat
}

const verifyResultTTL = 10 * time.Minute

// Verifier measures local models and asks the platform to judge verification status.
type Verifier struct {
	catalog           *Catalog
	server            *ServerClient
	serviceType       string
	hfHubCache        string
	modelPathOverride string

	mu          sync.Mutex
	cache       map[string]*fingerprintCache // alias -> weight fingerprint cache (vLLM)
	resultCache map[string]*verifyResultEntry
}

// NewVerifier creates a verifier. Measurements are sent to the server; expected hashes stay in the DB.
func NewVerifier(catalog *Catalog, server *ServerClient, serviceType, hfHubCache, modelPathOverride string) *Verifier {
	return &Verifier{
		catalog:           catalog,
		server:            server,
		serviceType:       strings.ToLower(serviceType),
		hfHubCache:        strings.TrimSpace(hfHubCache),
		modelPathOverride: strings.TrimSpace(modelPathOverride),
		cache:             make(map[string]*fingerprintCache),
		resultCache:       make(map[string]*verifyResultEntry),
	}
}

func (v *Verifier) resolveVLLMRoot(entry CatalogEntry, alias string) (string, error) {
	if v.modelPathOverride != "" {
		abs, err := filepath.Abs(v.modelPathOverride)
		if err != nil {
			return "", err
		}
		if dirHasWeights(abs) {
			return abs, nil
		}
	}

	repo := hfRepoForCatalog(alias, entry)
	ref := hfRefForCatalog(entry)
	hub := v.hfHubCache
	if hub == "" {
		var err error
		hub, err = DefaultHFHubCache()
		if err != nil {
			return "", err
		}
	}
	return ResolveHFModelRoot(hub, repo, ref)
}

// VerifyOllamaModel submits digest and size to the server for verification.
func (v *Verifier) VerifyOllamaModel(ctx context.Context, alias, digest string, sizeBytes int64) (Result, error) {
	res := Result{Alias: alias, Digest: NormalizeDigest(digest), SizeBytes: sizeBytes}

	if _, ok := v.catalog.Get(alias); !ok {
		res.Status = StatusUnverified
		return res, nil
	}

	if cached, ok := v.cachedOllamaResult(alias, res.Digest, sizeBytes); ok {
		return cached, nil
	}

	resp, err := v.server.VerifyModel(ctx, verifyModelRequest{
		Alias:     alias,
		Digest:    res.Digest,
		SizeBytes: sizeBytes,
	})
	if err != nil {
		res.Status = StatusFailed
		return res, err
	}
	applyServerResponse(&res, resp)
	v.storeOllamaResult(alias, res.Digest, sizeBytes, res)
	return res, nil
}

// VerifyVLLMModel hashes local weights and asks the server to verify.
func (v *Verifier) VerifyVLLMModel(ctx context.Context, alias string) (Result, error) {
	res := Result{Alias: alias}

	entry, ok := v.catalog.Get(alias)
	if !ok {
		res.Status = StatusUnverified
		return res, nil
	}

	root, err := v.resolveVLLMRoot(entry, alias)
	if err != nil {
		res.Status = StatusFailed
		return res, fmt.Errorf("locate weights for %s: %w", alias, err)
	}

	currentStats, err := weightDirStats(root)
	if err != nil {
		res.Status = StatusFailed
		return res, err
	}

	if cached, ok := v.cachedVLLMResult(alias, currentStats); ok {
		return cached, nil
	}

	files, stale, err := v.measureWithCache(alias, root)
	if err != nil {
		res.Status = StatusFailed
		return res, err
	}

	resp, err := v.server.VerifyModel(ctx, verifyModelRequest{
		Alias: alias,
		Files: files,
		Stale: stale,
	})
	if err != nil {
		res.Status = StatusFailed
		return res, err
	}
	applyServerResponse(&res, resp)
	v.storeVLLMResult(alias, currentStats, res)
	return res, nil
}

func (v *Verifier) cachedOllamaResult(alias, digest string, size int64) (Result, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	entry, ok := v.resultCache[alias]
	if !ok || !verifyResultFresh(entry) {
		return Result{}, false
	}
	if entry.digest != digest || entry.size != size {
		return Result{}, false
	}
	return entry.result, true
}

func (v *Verifier) storeOllamaResult(alias, digest string, size int64, res Result) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.resultCache[alias] = &verifyResultEntry{
		result:   res,
		cachedAt: time.Now(),
		digest:   digest,
		size:     size,
	}
}

func (v *Verifier) cachedVLLMResult(alias string, stats map[string]fileStat) (Result, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	entry, ok := v.resultCache[alias]
	if !ok || !verifyResultFresh(entry) {
		return Result{}, false
	}
	if !fileStatsEqual(entry.stats, stats) {
		return Result{}, false
	}
	return entry.result, true
}

func (v *Verifier) storeVLLMResult(alias string, stats map[string]fileStat, res Result) {
	v.mu.Lock()
	defer v.mu.Unlock()

	statsCopy := make(map[string]fileStat, len(stats))
	for k, val := range stats {
		statsCopy[k] = val
	}
	v.resultCache[alias] = &verifyResultEntry{
		result:   res,
		cachedAt: time.Now(),
		stats:    statsCopy,
	}
}

func (v *Verifier) clearVerifyResultCache() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.resultCache = make(map[string]*verifyResultEntry)
}

func verifyResultFresh(entry *verifyResultEntry) bool {
	return entry != nil && time.Since(entry.cachedAt) < verifyResultTTL
}

func (v *Verifier) measureWithCache(alias, root string) (files []FileMeasurement, stale bool, err error) {
	currentStats, err := weightDirStats(root)
	if err != nil {
		return nil, false, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	prev, hadCache := v.cache[alias]
	if hadCache && fileStatsEqual(prev.stats, currentStats) {
		return prev.files, false, nil
	}

	files, err = measureWeightDir(root)
	if err != nil {
		return nil, false, err
	}

	v.cache[alias] = &fingerprintCache{files: files, stats: currentStats}
	return files, hadCache, nil
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

func isKnownStatus(s string) bool {
	switch Status(strings.TrimSpace(s)) {
	case StatusVerified, StatusPending, StatusStale, StatusFailed, StatusUnverified:
		return true
	default:
		return false
	}
}

func applyServerResponse(res *Result, resp verifyModelResponse) {
	if !isKnownStatus(resp.VerificationStatus) {
		res.Status = StatusFailed
		return
	}
	res.Status = Status(strings.TrimSpace(resp.VerificationStatus))
	if resp.Digest != "" {
		res.Digest = NormalizeDigest(resp.Digest)
	}
	if resp.WeightFingerprint != "" {
		res.WeightFingerprint = strings.ToLower(strings.TrimSpace(resp.WeightFingerprint))
	}
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
			res, err := v.VerifyOllamaModel(ctx, m.ID, detail.Digest, detail.Size)
			if err != nil {
				logger.Error("Ollama verification error", zap.String("alias", m.ID), zap.Error(err))
				out[i].VerificationStatus = string(StatusFailed)
				continue
			}
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

// CatalogClient exposes the public model catalog.
func (v *Verifier) CatalogClient() *Catalog {
	return v.catalog
}

// RefreshCatalog reloads the public approved-model catalog and clears verify cache when it changes.
func (v *Verifier) RefreshCatalog(ctx context.Context) error {
	if v.catalog == nil {
		return nil
	}
	before := v.catalog.fingerprint()
	if err := v.catalog.Refresh(ctx); err != nil {
		return err
	}
	if v.catalog.fingerprint() != before {
		v.clearVerifyResultCache()
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
