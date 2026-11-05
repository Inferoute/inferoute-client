package benchmark

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunLevel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 4; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	got := RunLevel(context.Background(), srv.Client(), srv.URL, "Qwen/Qwen2.5-7B-Instruct", 2)
	if got.Errors != 0 || got.N != 2 {
		t.Fatalf("%+v", got)
	}
	if got.MinDecodeTPS <= 0 || got.AggregateTPS <= 0 {
		t.Fatalf("expected throughput, got %+v", got)
	}
	if strings.Contains(got.P95TTFT.String(), "fail") {
		t.Fatal(got.P95TTFT)
	}
}
