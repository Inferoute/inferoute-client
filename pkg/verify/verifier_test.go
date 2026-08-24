package verify

import (
	"testing"
)

func TestAggregateFingerprintDeterministic(t *testing.T) {
	files := []FileMeasurement{
		{Name: "weights.safetensors", Hash: "bbb", HashMethod: "safetensors_header", Size: 10},
		{Name: "config.json", Hash: "aaa", HashMethod: "full", Size: 5},
	}

	fp1 := AggregateFingerprint(files)
	fp2 := AggregateFingerprint([]FileMeasurement{
		files[1],
		files[0],
	})
	if fp1 != fp2 {
		t.Fatalf("fingerprint not order-independent: %s vs %s", fp1, fp2)
	}
	if len(fp1) != 64 {
		t.Fatalf("expected sha256 hex, got %q", fp1)
	}

	different := AggregateFingerprint([]FileMeasurement{
		{Name: "config.json", Hash: "ccc", HashMethod: "full", Size: 5},
	})
	if different == fp1 {
		t.Fatal("expected different fingerprint for different hashes")
	}
}

func TestRevisionFromSnapshotPath(t *testing.T) {
	got := revisionFromSnapshotPath("/cache/hub/models--Org--Name/snapshots/abc123def")
	if got != "abc123def" {
		t.Fatalf("got %q, want abc123def", got)
	}
	if got := revisionFromSnapshotPath("/flat/local/dir"); got != "" {
		t.Fatalf("flat dir should have empty revision, got %q", got)
	}
}
