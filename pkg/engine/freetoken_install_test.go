package engine

import (
	"archive/zip"
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

func TestExtractNamedFromZip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "tools.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("nested/uv.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("uv-bin")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "uv.exe")
	if err := extractNamedFromZip(zipPath, dest, "uv.exe"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "uv-bin" {
		t.Fatalf("got %q", got)
	}
}
