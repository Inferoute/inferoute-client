package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// shortcut: cache names are `<repo with / → _>_<hf filename>`, pinned to llama.cpp b11770.
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
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(strings.ToLower(name), ".gguf") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, manifestFile{
			rel:     strings.TrimPrefix(name, repoKey),
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
