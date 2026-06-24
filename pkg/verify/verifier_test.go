package verify

import (
	"testing"
	"time"
)

func TestApplyServerResponseKnownStatus(t *testing.T) {
	res := Result{Alias: "test"}
	applyServerResponse(&res, verifyModelResponse{VerificationStatus: "verified"})
	if res.Status != StatusVerified {
		t.Fatalf("status = %q, want verified", res.Status)
	}
}

func TestApplyServerResponseUnknownStatus(t *testing.T) {
	res := Result{Alias: "test"}
	applyServerResponse(&res, verifyModelResponse{VerificationStatus: "hacked"})
	if res.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", res.Status)
	}
}

func TestApplyServerResponseEmptyStatus(t *testing.T) {
	res := Result{Alias: "test"}
	applyServerResponse(&res, verifyModelResponse{})
	if res.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", res.Status)
	}
}

func TestVerifyResultCacheOllamaHitAndInvalidate(t *testing.T) {
	v := &Verifier{resultCache: make(map[string]*verifyResultEntry)}
	want := Result{Alias: "gguf/foo", Status: StatusVerified, Digest: "abc", SizeBytes: 42}
	v.storeOllamaResult("gguf/foo", "abc", 42, want)

	got, ok := v.cachedOllamaResult("gguf/foo", "abc", 42)
	if !ok || got.Status != StatusVerified {
		t.Fatalf("expected cache hit, got ok=%v status=%q", ok, got.Status)
	}

	if _, ok := v.cachedOllamaResult("gguf/foo", "changed", 42); ok {
		t.Fatal("expected cache miss after digest change")
	}
}

func TestVerifyResultCacheTTLExpiry(t *testing.T) {
	v := &Verifier{resultCache: make(map[string]*verifyResultEntry)}
	v.resultCache["gguf/foo"] = &verifyResultEntry{
		result:   Result{Alias: "gguf/foo", Status: StatusVerified},
		cachedAt: time.Now().Add(-verifyResultTTL - time.Second),
		digest:   "abc",
		size:     1,
	}

	if _, ok := v.cachedOllamaResult("gguf/foo", "abc", 1); ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestVerifyResultCacheVLLMWeightChange(t *testing.T) {
	v := &Verifier{resultCache: make(map[string]*verifyResultEntry)}
	stats := map[string]fileStat{"model.safetensors": {size: 100, modTime: 1}}
	want := Result{Alias: "org/model", Status: StatusPending}
	v.storeVLLMResult("org/model", stats, want)

	got, ok := v.cachedVLLMResult("org/model", stats)
	if !ok || got.Status != StatusPending {
		t.Fatalf("expected cache hit, got ok=%v status=%q", ok, got.Status)
	}

	changed := map[string]fileStat{"model.safetensors": {size: 101, modTime: 1}}
	if _, ok := v.cachedVLLMResult("org/model", changed); ok {
		t.Fatal("expected cache miss after weight stats change")
	}
}
