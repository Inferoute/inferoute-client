package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVerifyRequestShape(t *testing.T) {
	got, err := json.Marshal(verifyModelRequest{
		Alias: "qwen2.5-7b-instruct", ServiceType: "vllm", SizeBytes: 100,
		Files: []FileMeasurement{{Name: "model.safetensors", Hash: "abc", HashMethod: "sha256", Size: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "contract", "testdata", "verify-model.request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("verify payload drifted\ngot  %s\nwant %s", got, want)
	}
}
