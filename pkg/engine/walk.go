package engine

import (
	"os"
	"path/filepath"
	"strings"
)

func walkFor(root, name string, maxDepth int) string {
	if root == "" || maxDepth < 0 {
		return ""
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if found != "" {
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil && rel != "." {
			n := strings.Count(rel, string(os.PathSeparator))
			if n > maxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if !d.IsDir() && d.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
