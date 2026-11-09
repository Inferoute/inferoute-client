package capprobe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportRegistersThenRecords(t *testing.T) {
	var got capabilityReport
	registered := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/provider/capabilities/deployment":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"revision":0,"fingerprint":"","attempt":0}`))
				return
			}
			registered++
			var req struct {
				Fingerprint  string `json:"fingerprint"`
				PrevRevision int64  `json:"prev_revision"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.Fingerprint != "fp" || req.PrevRevision != 0 {
				t.Fatalf("register %+v", req)
			}
			_, _ = w.Write([]byte(`{"revision":1,"fingerprint":"fp","attempt":0}`))
		case "/api/provider/capabilities":
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	err := report(context.Background(), srv.Client(), srv.URL, "k", "fp", []Outcome{
		Supported, Unsupported, Waiting, Supported, Unverified,
	})
	if err != nil {
		t.Fatal(err)
	}
	if registered != 1 || got.Revision != 1 || got.Attempt != 1 || got.Fingerprint != "fp" {
		t.Fatalf("registered=%d got=%+v", registered, got)
	}
	if got.Tools == nil || !*got.Tools || got.ForcedTools == nil || *got.ForcedTools {
		t.Fatalf("tools %+v forced %+v", got.Tools, got.ForcedTools)
	}
	if got.JSONObject != nil || got.Logprobs != nil || got.JSONSchema == nil || !*got.JSONSchema {
		t.Fatalf("partial %+v", got)
	}
}

func TestReportSameFingerprintSkipsRegister(t *testing.T) {
	var got capabilityReport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/provider/capabilities/deployment":
			if r.Method != http.MethodGet {
				t.Fatal("registered again")
			}
			_, _ = w.Write([]byte(`{"revision":4,"fingerprint":"fp","attempt":3}`))
		case "/api/provider/capabilities":
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	err := report(context.Background(), srv.Client(), srv.URL, "k", "fp", []Outcome{Supported})
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 4 || got.Attempt != 4 {
		t.Fatalf("%+v", got)
	}
}
