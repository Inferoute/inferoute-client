package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWeightFingerprintDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.json", `{"model_type":"test"}`)
	writeFile(t, dir, "weights.safetensors", safetensorsFixture())

	manifest := []ManifestEntry{
		{Name: "config.json", HashMethod: "full", Size: int64(len(`{"model_type":"test"}`))},
		{Name: "weights.safetensors", HashMethod: "safetensors_header", Size: int64(len(safetensorsFixture()))},
	}

	fp1, err := WeightFingerprint(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	fp2, err := WeightFingerprint(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %s vs %s", fp1, fp2)
	}
	if len(fp1) != 64 {
		t.Fatalf("expected sha256 hex, got %q", fp1)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// minimal valid safetensors header for hash smoke test
func safetensorsFixture() string {
	header := `{"meta":{},"tensors":{}}`
	// 8-byte LE length + header JSON
	var b []byte
	length := uint64(len(header))
	b = append(b, byte(length), byte(length>>8), byte(length>>16), byte(length>>24), byte(length>>32), byte(length>>40), byte(length>>48), byte(length>>56))
	b = append(b, header...)
	return string(b)
}

func TestNormalizeDigest(t *testing.T) {
	if got := NormalizeDigest("sha256:AbC"); got != "abc" {
		t.Fatalf("got %q", got)
	}
}
