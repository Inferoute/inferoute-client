package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// measureWeightDir hashes top-level files in a vLLM weight directory.
func measureWeightDir(root string) ([]FileMeasurement, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read weight dir: %w", err)
	}

	var files []FileMeasurement
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(root, name)
		method := "full"
		if strings.HasSuffix(name, ".safetensors") {
			method = "safetensors_header"
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		hash, err := FileHash(path, method)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", name, err)
		}
		files = append(files, FileMeasurement{
			Name:       name,
			Hash:       hash,
			HashMethod: method,
			Size:       info.Size(),
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no weight files in %s", root)
	}
	return files, nil
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
