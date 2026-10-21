package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureGGUFShardPrefix(t *testing.T) {
	dir := t.TempDir()
	repo := "Qwen/Qwen2.5-7B-Instruct-GGUF"
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Qwen_Qwen2.5-7B-Instruct-GGUF_qwen-Q4_K_M-00001-of-00002.gguf")
	write("Qwen_Qwen2.5-7B-Instruct-GGUF_qwen-Q4_K_M-00002-of-00002.gguf")
	write("Qwen_Qwen2.5-7B-Instruct-GGUF_qwen-Q8_0.gguf")

	files, err := measureGGUF(dir, repo, "qwen-Q4_K_M.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].HashMethod != "full" || files[0].Name == "" {
		t.Fatalf("measurement = %+v", files[0])
	}
}

func TestMeasureGGUFHuggingFaceName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gpt-oss-20b-MXFP4.gguf"), []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := measureGGUF(dir, "ggml-org/gpt-oss-20b-GGUF", "gpt-oss-20b-MXFP4.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "gpt-oss-20b-MXFP4.gguf" {
		t.Fatalf("files = %+v", files)
	}
}

func TestResolveGGUFSnapshot(t *testing.T) {
	hub := t.TempDir()
	t.Setenv("HF_HUB_CACHE", hub)
	snap := filepath.Join(hub, "models--ggml-org--gpt-oss-20b-GGUF", "snapshots", "abc123")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hub, "models--ggml-org--gpt-oss-20b-GGUF", "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap, "gpt-oss-20b-MXFP4.gguf"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub, "models--ggml-org--gpt-oss-20b-GGUF", "refs", "main"), []byte("abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveGGUFSnapshot("ggml-org/gpt-oss-20b-GGUF", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != snap {
		t.Fatalf("snapshot = %s", got)
	}
}