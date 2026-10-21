package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// --hf-repo stores the file under the HuggingFace hub snapshot, original name.
// The flat `<repo with / → _>_<filename>` cache is only a fallback.
var ggufShardName = regexp.MustCompile(`^(.+)-\d+-of-\d+\.gguf$`)

func ggufName(entry CatalogEntry) string {
	if entry.GGUFFile != nil {
		return strings.TrimSpace(*entry.GGUFFile)
	}
	return ""
}

// DefaultLlamaCacheDir mirrors llama.cpp's cache directory.
func DefaultLlamaCacheDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("LLAMA_CACHE")); v != "" {
		return filepath.Abs(v)
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA is unset")
		}
		return filepath.Join(base, "llama.cpp"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Caches", "llama.cpp"), nil
	default:
		if v := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); v != "" {
			return filepath.Join(v, "llama.cpp"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cache", "llama.cpp"), nil
	}
}

func llamaCachePrefix(repo, ggufFile string) (string, error) {
	repo = strings.TrimSpace(repo)
	base := filepath.Base(strings.TrimSpace(ggufFile))
	if repo == "" || !strings.HasSuffix(base, ".gguf") {
		return "", fmt.Errorf("llama.cpp catalog row needs hf_repo and gguf_file")
	}
	stem := strings.TrimSuffix(base, ".gguf")
	if m := ggufShardName.FindStringSubmatch(base); m != nil {
		stem = m[1]
	}
	return strings.ReplaceAll(repo, "/", "_") + "_" + stem, nil
}

func listGGUF(cacheDir, repo, ggufFile string) ([]manifestFile, error) {
	prefix, err := llamaCachePrefix(repo, ggufFile)
	if err != nil {
		return nil, err
	}
	repoKey := strings.ReplaceAll(strings.TrimSpace(repo), "/", "_") + "_"
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("read llama.cpp cache: %w", err)
	}
	var files []manifestFile
	for _, e := range entries {
		if e.IsDir() || !wantedGGUF(e.Name(), prefix, ggufFile) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		name := e.Name()
		rel := name
		if strings.HasPrefix(name, repoKey) {
			rel = strings.TrimPrefix(name, repoKey)
		}
		files = append(files, manifestFile{
			rel:     rel,
			abs:     filepath.Join(cacheDir, name),
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no GGUF for %s in %s", ggufFile, cacheDir)
	}
	return files, nil
}

func wantedGGUF(name, prefix, ggufFile string) bool {
	base := filepath.Base(name)
	if !strings.HasSuffix(strings.ToLower(base), ".gguf") {
		return false
	}
	if base == filepath.Base(strings.TrimSpace(ggufFile)) {
		return true
	}
	stem := strings.TrimSuffix(filepath.Base(strings.TrimSpace(ggufFile)), ".gguf")
	if m := ggufShardName.FindStringSubmatch(base); m != nil && m[1] == stem {
		return true
	}
	return prefix != "" && strings.HasPrefix(base, prefix)
}

// resolveGGUFSnapshot finds the HuggingFace hub snapshot llama-server filled via --hf-repo.
func resolveGGUFSnapshot(repo, revision string) (string, error) {
	hub, err := DefaultHFHubCache()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(hub, HFRepoToCacheDir(strings.TrimSpace(repo)))
	try := []string{strings.TrimSpace(revision), "main"}
	for _, rev := range try {
		if rev == "" {
			continue
		}
		if snap, err := ggufSnapshot(cacheDir, rev); err == nil {
			return snap, nil
		}
	}
	snapshots := filepath.Join(cacheDir, "snapshots")
	entries, err := os.ReadDir(snapshots)
	if err != nil {
		return "", fmt.Errorf("no GGUF snapshot for %s: %w", repo, err)
	}
	var found string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(snapshots, e.Name())
		if !dirHasGGUF(candidate) {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("multiple GGUF snapshots under %s", cacheDir)
		}
		found = candidate
	}
	if found == "" {
		return "", fmt.Errorf("no GGUF snapshot for %s", repo)
	}
	return found, nil
}

func ggufSnapshot(cacheDir, revision string) (string, error) {
	snap := filepath.Join(cacheDir, "snapshots", revision)
	if dirHasGGUF(snap) {
		return snap, nil
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, "refs", revision))
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(data))
	snap = filepath.Join(cacheDir, "snapshots", sha)
	if sha != "" && dirHasGGUF(snap) {
		return snap, nil
	}
	return "", fmt.Errorf("revision %s not found under %s", revision, cacheDir)
}

func dirHasGGUF(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			return true
		}
	}
	return false
}

func ggufStats(cacheDir, repo, ggufFile string) (map[string]fileStat, error) {
	listed, err := listGGUF(cacheDir, repo, ggufFile)
	if err != nil {
		return nil, err
	}
	stats := make(map[string]fileStat, len(listed))
	for _, mf := range listed {
		stats[mf.rel] = fileStat{size: mf.size, modTime: mf.modTime}
	}
	return stats, nil
}

func measureGGUF(cacheDir, repo, ggufFile string) ([]FileMeasurement, error) {
	listed, err := listGGUF(cacheDir, repo, ggufFile)
	if err != nil {
		return nil, err
	}
	files := make([]FileMeasurement, 0, len(listed))
	for _, mf := range listed {
		hash, err := FileHash(mf.abs, "full")
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", mf.rel, err)
		}
		files = append(files, FileMeasurement{
			Name:       mf.rel,
			Hash:       hash,
			HashMethod: "full",
			Size:       mf.size,
		})
	}
	return files, nil
}
