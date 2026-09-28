package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureWeightDirIncludesPoolingSkipsOnnx(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config.json", `{"arch":"x"}`)
	writeFile(t, root, "pytorch_model.bin", "weights")
	writeFile(t, root, "README.md", "ignore me")
	if err := os.Mkdir(filepath.Join(root, "1_Pooling"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "1_Pooling"), "config.json", `{"pooling":"cls"}`)
	if err := os.Mkdir(filepath.Join(root, "onnx"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "onnx"), "config.json", `{"onnx":true}`)

	files, err := measureWeightDir(root)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]struct{}{}
	for _, f := range files {
		got[f.Name] = struct{}{}
	}
	for _, name := range []string{"config.json", "pytorch_model.bin", "1_Pooling/config.json"} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing %s in %#v", name, got)
		}
	}
	if _, ok := got["onnx/config.json"]; ok {
		t.Fatal("onnx/config.json should be skipped")
	}
	if _, ok := got["README.md"]; ok {
		t.Fatal("README.md should be skipped")
	}
}

func TestMeasureWeightDirSkipsAltFormatTrees(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config.json", `{}`)
	writeFile(t, root, "pytorch_model.bin", "root-weights")
	for _, dir := range []string{"original", "metal", "mlx"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "original"), "model.safetensors", "dup")
	writeFile(t, filepath.Join(root, "metal"), "model.bin", "apple")
	writeFile(t, filepath.Join(root, "mlx"), "model.safetensors", "mlx")

	files, err := measureWeightDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]struct{}{}
	for _, f := range files {
		got[f.Name] = struct{}{}
	}
	if _, ok := got["config.json"]; !ok {
		t.Fatal("missing root config.json")
	}
	if _, ok := got["pytorch_model.bin"]; !ok {
		t.Fatal("missing root weights")
	}
	for _, name := range []string{"original/model.safetensors", "metal/model.bin", "mlx/model.safetensors"} {
		if _, ok := got[name]; ok {
			t.Fatalf("%s should be skipped: %#v", name, got)
		}
	}
}

func TestWeightDirStatsTracksNestedManifestFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config.json", `{"arch":"x"}`)
	if err := os.Mkdir(filepath.Join(root, "1_Pooling"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "1_Pooling"), "config.json", `{"pooling":"cls"}`)

	stats, err := weightDirStats(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stats["1_Pooling/config.json"]; !ok {
		t.Fatalf("stats missing nested file: %#v", stats)
	}
}
