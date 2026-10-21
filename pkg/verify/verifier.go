package verify

import (
	"context"
	"fmt"
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

// VerifyVLLMModel hashes local weights and asks the server to verify.
func (v *Verifier) VerifyVLLMModel(ctx context.Context, alias string) (Result, error) {
	res := Result{Alias: alias}

	entry, ok := v.catalog.Get(alias)
	if !ok {
		res.Status = StatusUnverified
		return res, nil
	}

	var root string
	var err error
	if v.serviceType == "llama_cpp" {
		root, err = v.resolveLlamaRoot(entry)
	} else {
		root, err = v.resolveVLLMRoot(entry, alias)
	}
	if err != nil {
		res.Status = StatusFailed
		return res, fmt.Errorf("locate weights for %s: %w", alias, err)
	}

	currentStats, err := v.weightStats(alias, root)
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
		Alias:       alias,
		ServiceType: v.serviceType,
		Files:       files,
		Stale:       stale,
	})
	if err != nil {
		res.Status = StatusFailed
		return res, err
	}
	applyServerResponse(&res, resp)
	v.storeVLLMResult(alias, currentStats, res)
	return res, nil
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
	currentStats, err := v.weightStats(alias, root)
	if err != nil {
		return nil, false, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	prev, hadCache := v.cache[alias]
	if hadCache && fileStatsEqual(prev.stats, currentStats) {
		return prev.files, false, nil
	}

	if v.serviceType == "llama_cpp" {
		entry, _ := v.catalog.Get(alias)
		files, err = measureGGUF(root, hfRepoForCatalog(alias, entry), ggufName(entry))
	} else {
		files, err = measureWeightDir(root)
	}
	if err != nil {
		return nil, false, err
	}

	v.cache[alias] = &fingerprintCache{files: files, stats: currentStats}
	return files, hadCache, nil
}

func (v *Verifier) resolveLlamaRoot(entry CatalogEntry) (string, error) {
	if v.modelPathOverride != "" {
		return filepath.Abs(v.modelPathOverride)
	}
	return DefaultLlamaCacheDir()
}

func (v *Verifier) weightStats(alias, root string) (map[string]fileStat, error) {
	if v.serviceType != "llama_cpp" {
		return weightDirStats(root)
	}
	entry, _ := v.catalog.Get(alias)
	return ggufStats(root, hfRepoForCatalog(alias, entry), ggufName(entry))
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
	if resp.WeightFingerprint != "" {
		res.WeightFingerprint = strings.ToLower(strings.TrimSpace(resp.WeightFingerprint))
	}
}

// ApplyToModels enriches discovered models with verification fields.
func (v *Verifier) ApplyToModels(ctx context.Context, llmClient llm.Client, models []llm.Model) []llm.Model {
	out := make([]llm.Model, len(models))
	for i, m := range models {
		out[i] = m
		switch v.serviceType {
		case "vllm", "llama_cpp":
			res, err := v.VerifyVLLMModel(ctx, m.ID)
			if err != nil {
				logger.Error("vLLM verification error", zap.String("alias", m.ID), zap.Error(err))
				out[i].VerificationStatus = string(StatusFailed)
				continue
			}
			applyResult(&out[i], res)
			entry, ok := v.catalog.Get(m.ID)
			if ok {
				applyContextGate(ctx, llmClient, &out[i], entry)
			}
		default:
			out[i].VerificationStatus = string(StatusUnverified)
		}
	}
	return out
}

func applyResult(m *llm.Model, res Result) {
	m.VerificationStatus = string(res.Status)
	m.WeightFingerprint = res.WeightFingerprint
	m.SizeBytes = res.SizeBytes
}

// IsInferenceAllowed returns true when the model may serve traffic.
func IsInferenceAllowed(status string) bool {
	return status == string(StatusVerified)
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
