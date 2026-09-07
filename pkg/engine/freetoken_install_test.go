package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindFreeTokenWindowsDesktopDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("ProgramFiles", filepath.Join(root, "pf"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))

	bin := filepath.Join(root, "Programs", "FreeToken Desktop", "resources", "ft.exe")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findFreeTokenWindows()
	if got != bin {
		t.Fatalf("findFreeTokenWindows() = %q, want %q", got, bin)
	}
}

func TestFindFreeTokenWindowsVenv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("ProgramFiles", filepath.Join(root, "pf"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))

	bin := filepath.Join(root, "inferoute", "venv-freetoken", "Scripts", "ft.exe")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findFreeTokenWindows()
	if got != bin {
		t.Fatalf("findFreeTokenWindows() = %q, want %q", got, bin)
	}
}

func TestParseFreeTokenManifest(t *testing.T) {
	t.Parallel()
	data := []byte(`{
		"runtime": {"name": "freetoken.whl", "url": "https://example/freetoken.whl"},
		"kernel_cache": {"name": "cache.whl", "url": "https://example/cache.whl"}
	}`)
	man, err := parseFreeTokenManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if man.Runtime.URL != "https://example/freetoken.whl" || man.KernelCache.Name != "cache.whl" {
		t.Fatalf("manifest = %+v", man)
	}
}

func TestParseFreeTokenManifestMissingURL(t *testing.T) {
	t.Parallel()
	_, err := parseFreeTokenManifest([]byte(`{"runtime":{"name":"x"}}`))
	if err == nil {
		t.Fatal("expected error")
	}
}
