package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHFRepoToCacheDir(t *testing.T) {
	if got := HFRepoToCacheDir("Qwen/Qwen3-0.6B"); got != "models--Qwen--Qwen3-0.6B" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveHFModelRootPinnedRevision(t *testing.T) {
	hub := t.TempDir()
	repo := "Qwen/Qwen3-0.6B"
	rev := "abc123deadbeef"
	snap := filepath.Join(hub, HFRepoToCacheDir(repo), "snapshots", rev)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap, "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := ResolveHFModelRoot(hub, repo, rev)
	if err != nil {
		t.Fatal(err)
	}
	if root != snap {
		t.Fatalf("got %q want %q", root, snap)
	}
}

func TestResolveHFModelRootRevisionMainRef(t *testing.T) {
	hub := t.TempDir()
	repo := "Qwen/Qwen3-0.6B"
	sha := "abc123deadbeef"
	cacheDir := filepath.Join(hub, HFRepoToCacheDir(repo))

	refsDir := filepath.Join(cacheDir, "refs")
	if err := os.MkdirAll(refsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refsDir, "main"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := filepath.Join(cacheDir, "snapshots", sha)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap, "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := ResolveHFModelRoot(hub, repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	if root != snap {
		t.Fatalf("got %q want %q", root, snap)
	}
}

func TestResolveHFModelRootExplicitFlatDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !dirHasWeights(dir) {
		t.Fatal("expected weights dir")
	}
}
