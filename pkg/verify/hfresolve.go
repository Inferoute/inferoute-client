package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultHFHubCache returns the standard HuggingFace hub cache directory.
func DefaultHFHubCache() (string, error) {
	if v := strings.TrimSpace(os.Getenv("HF_HUB_CACHE")); v != "" {
		return filepath.Abs(v)
	}
	if v := strings.TrimSpace(os.Getenv("HUGGINGFACE_HUB_CACHE")); v != "" {
		return filepath.Abs(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "huggingface", "hub"), nil
}

// HFRepoToCacheDir converts Qwen/Qwen3-0.6B to models--Qwen--Qwen3-0.6B.
func HFRepoToCacheDir(repoID string) string {
	return "models--" + strings.ReplaceAll(repoID, "/", "--")
}

func dirHasWeights(root string) bool {
	if st, err := os.Stat(filepath.Join(root, "config.json")); err == nil && !st.IsDir() {
		return true
	}
	return false
}

// ResolveHFModelRoot locates on-disk weights for a HuggingFace repo in the hub cache.
// hubCache is typically ~/.cache/huggingface/hub; revision is the pinned commit SHA.
func ResolveHFModelRoot(hubCache, repoID, revision string) (string, error) {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return "", fmt.Errorf("empty huggingface repo id")
	}

	if hubCache == "" {
		var err error
		hubCache, err = DefaultHFHubCache()
		if err != nil {
			return "", err
		}
	}
	hubCache, err := filepath.Abs(hubCache)
	if err != nil {
		return "", err
	}

	cacheDir := filepath.Join(hubCache, HFRepoToCacheDir(repoID))
	if revision != "" {
		return snapshotForRevision(cacheDir, revision)
	}

	// No pinned revision: use refs/main if present, else the only snapshot.
	if snap, err := snapshotForRevision(cacheDir, "main"); err == nil {
		return snap, nil
	}

	snapshotsDir := filepath.Join(cacheDir, "snapshots")
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return "", fmt.Errorf("no snapshots under %s: %w", cacheDir, err)
	}
	var found string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(snapshotsDir, e.Name())
		if !dirHasWeights(candidate) {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("multiple snapshots under %s; pin hf_revision in approved builds", cacheDir)
		}
		found = candidate
	}
	if found == "" {
		return "", fmt.Errorf("no weight files found under %s", cacheDir)
	}
	return found, nil
}

// snapshotForRevision resolves a HF hub revision to a snapshots/ directory.
// revision may be a commit SHA (snapshots/<sha>) or a ref name (refs/<name> → snapshots/<sha>).
func snapshotForRevision(cacheDir, revision string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", fmt.Errorf("empty revision")
	}

	snap := filepath.Join(cacheDir, "snapshots", revision)
	if dirHasWeights(snap) {
		return snap, nil
	}

	refFile := filepath.Join(cacheDir, "refs", revision)
	data, err := os.ReadFile(refFile)
	if err != nil {
		return "", fmt.Errorf("revision %s not found under %s", revision, cacheDir)
	}
	sha := strings.TrimSpace(string(data))
	if sha == "" {
		return "", fmt.Errorf("revision %s not found under %s", revision, cacheDir)
	}
	snap = filepath.Join(cacheDir, "snapshots", sha)
	if dirHasWeights(snap) {
		return snap, nil
	}
	return "", fmt.Errorf("revision %s not found under %s", revision, cacheDir)
}

func hfRepoForBuild(alias string, build ApprovedBuild) string {
	if build.HFRepo != nil && strings.TrimSpace(*build.HFRepo) != "" {
		return strings.TrimSpace(*build.HFRepo)
	}
	return alias
}

func hfRevisionForBuild(build ApprovedBuild) string {
	if build.HFRevision != nil {
		return strings.TrimSpace(*build.HFRevision)
	}
	return ""
}
