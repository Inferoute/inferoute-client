package verify

import "testing"

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
