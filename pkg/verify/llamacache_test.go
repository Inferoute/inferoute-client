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