package engine

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestFindFreeTokenWindowsIgnoresDesktop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("ProgramFiles", filepath.Join(root, "pf"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))

	desktop := filepath.Join(root, "Programs", "FreeToken Desktop", "resources", "ft.exe")
	writeFile(t, desktop, []byte("x"))

	got := findFreeTokenWindows()
	if got == desktop || isFreeTokenDesktopBin(got) {
		t.Fatalf("findFreeTokenWindows() = %q, Desktop stub must be ignored", got)
	}
}

func TestFindFreeTokenWindowsVenv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("ProgramFiles", filepath.Join(root, "pf"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))

	desktop := filepath.Join(root, "Programs", "FreeToken Desktop", "resources", "ft.exe")
	writeFile(t, desktop, []byte("x"))
	bin := filepath.Join(root, "inferoute", "venv-freetoken", "Scripts", "ft.exe")
	writeFile(t, bin, []byte("x"))

	got := findFreeTokenWindows()
	if got != bin {
		t.Fatalf("findFreeTokenWindows() = %q, want %q", got, bin)
	}
}

func TestResolveBinIgnoresDesktop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("ProgramFiles", filepath.Join(root, "pf"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))

	desktop := filepath.Join(root, "Programs", "FreeToken Desktop", "resources", "ft.exe")
	writeFile(t, desktop, []byte("x"))
	venv := filepath.Join(root, "inferoute", "venv-freetoken", "Scripts", "ft.exe")
	writeFile(t, venv, []byte("x"))

	if got := ResolveBin(KindFreeToken, desktop); got != venv {
		t.Fatalf("ResolveBin(desktop) = %q, want %q", got, venv)
	}
}

func TestIsFreeTokenDesktopBin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{`C:\Users\x\AppData\Local\Programs\FreeToken Desktop\resources\ft.exe`, true},
		{`C:\Users\x\AppData\Local\Programs\freetoken-desktop\ft.exe`, true},
		{`C:\Users\x\AppData\Local\inferoute\venv-freetoken\Scripts\ft.exe`, false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isFreeTokenDesktopBin(tt.path); got != tt.want {
			t.Errorf("isFreeTokenDesktopBin(%q) = %v, want %v", tt.path, got, tt.want)
		}
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

func TestCUDATorchUVArgs(t *testing.T) {
	t.Parallel()
	py := `C:\Users\x\AppData\Local\inferoute\venv-freetoken\Scripts\python.exe`
	got := cudaTorchUVArgs(py, "2.11.0")
	want := []string{
		"pip", "install", "--python", py, "--upgrade", "--reinstall",
		"torch==2.11.0", "--index-url", pytorchCUDAIndexURL,
	}
	if len(got) != len(want) {
		t.Fatalf("cudaTorchUVArgs(%q, 2.11.0) = %v, want %v", py, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cudaTorchUVArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCUDATorchPipArgsUnpinned(t *testing.T) {
	t.Parallel()
	got := cudaTorchPipArgs("")
	want := []string{"install", "--upgrade", "--force-reinstall", "torch", "--index-url", pytorchCUDAIndexURL}
	if len(got) != len(want) {
		t.Fatalf("cudaTorchPipArgs(\"\") = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cudaTorchPipArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTorchBaseVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"2.11.0+cpu", "2.11.0"},
		{"2.11.0+cu130", "2.11.0"},
		{"2.11.0", "2.11.0"},
		{" 2.11.0+cpu\n", "2.11.0"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := torchBaseVersion(tt.in); got != tt.want {
			t.Errorf("torchBaseVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTorchSpec(t *testing.T) {
	t.Parallel()
	if got := torchSpec("2.11.0"); got != "torch==2.11.0" {
		t.Errorf("torchSpec(2.11.0) = %q, want torch==2.11.0", got)
	}
	if got := torchSpec(""); got != "torch" {
		t.Errorf("torchSpec(\"\") = %q, want torch", got)
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

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
}
