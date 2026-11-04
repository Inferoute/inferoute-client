//go:build !windows

package inferscale

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootOwnedRejectsUserFileAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inferscale")
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if err := rootOwned(path); err == nil {
			t.Fatal("user-owned file accepted")
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := rootOwned(link); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := ancestorsLocked(dir); err == nil {
		t.Fatal("user-writable directory accepted")
	}
}
