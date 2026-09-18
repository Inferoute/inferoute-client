package verify

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Vendor/cache trees vLLM does not load. Matching getModels + bootstrap.
var skipManifestDirs = map[string]struct{}{
	".git":     {},
	".cache":   {},
	"onnx":     {},
	"onnx-gpu": {},
	"openvino": {},
}

type manifestFile struct {
	rel     string
	abs     string
	size    int64
	modTime int64
}

// measureWeightDir hashes manifest files under a vLLM weight directory.
// Walks nested dirs (e.g. 1_Pooling/) but skips onnx/ and other vendor trees.
func measureWeightDir(root string) ([]FileMeasurement, error) {
	listed, err := listManifestFiles(root)
	if err != nil {
		return nil, err
	}

	files := make([]FileMeasurement, 0, len(listed))
	for _, mf := range listed {
		method := "full"
		if strings.HasSuffix(mf.rel, ".safetensors") {
			method = "safetensors_header"
		}
		hash, err := FileHash(mf.abs, method)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", mf.rel, err)
		}
		files = append(files, FileMeasurement{
			Name:       mf.rel,
			Hash:       hash,
			HashMethod: method,
			Size:       mf.size,
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no weight files in %s", root)
	}
	return files, nil
}

func weightDirStats(root string) (map[string]fileStat, error) {
	listed, err := listManifestFiles(root)
	if err != nil {
		return nil, err
	}
	stats := make(map[string]fileStat, len(listed))
	for _, mf := range listed {
		stats[mf.rel] = fileStat{size: mf.size, modTime: mf.modTime}
	}
	return stats, nil
}

func listManifestFiles(root string) ([]manifestFile, error) {
	root = filepath.Clean(root)
	var files []manifestFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			if _, skip := skipManifestDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !isManifestFile(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		files = append(files, manifestFile{
			rel:     filepath.ToSlash(rel),
			abs:     path,
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read weight dir: %w", err)
	}
	return files, nil
}

func isManifestFile(name string) bool {
	if name == ".DS_Store" {
		return false
	}
	switch name {
	case "config.json",
		"generation_config.json",
		"tokenizer.json",
		"tokenizer_config.json",
		"vocab.json",
		"merges.txt",
		"special_tokens_map.json",
		"added_tokens.json":
		return true
	}
	if strings.HasSuffix(name, ".safetensors") {
		return true
	}
	if strings.HasPrefix(name, "pytorch_model") && strings.HasSuffix(name, ".bin") {
		return true
	}
	if strings.HasPrefix(name, "model") && strings.HasSuffix(name, ".bin") {
		return true
	}
	return false
}

func hfRepoForCatalog(alias string, entry CatalogEntry) string {
	if entry.HFRepo != nil && strings.TrimSpace(*entry.HFRepo) != "" {
		return strings.TrimSpace(*entry.HFRepo)
	}
	return alias
}

func hfRefForCatalog(entry CatalogEntry) string {
	if entry.HFRef != nil {
		return strings.TrimSpace(*entry.HFRef)
	}
	return ""
}
