package engine

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceVenvDirRemovesExisting(t *testing.T) {
	dir := t.TempDir()
	venv := filepath.Join(dir, ".venv-vllm-metal")
	if err := os.Mkdir(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := replaceVenvDir(&out, venv); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(venv); !os.IsNotExist(err) {
		t.Fatalf("venv still exists: %v", err)
	}
	if !strings.Contains(out.String(), venv) {
		t.Fatalf("stdout = %q, want venv path", out.String())
	}
}

func TestReplaceVenvDirMissingOK(t *testing.T) {
	if err := replaceVenvDir(io.Discard, filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
}
