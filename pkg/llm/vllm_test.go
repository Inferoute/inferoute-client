package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func hangUntilCancel(w http.ResponseWriter, r *http.Request) {
	// The server only notices the client has gone when the body has been
	// read. A handler blocked before that does not see r.Context() cancel.
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()
	<-r.Context().Done()
}

// A stream with no caller deadline must still die with the client timeout.
// The unlimited stream client would leave a stalled engine holding a slot.
func TestStreamWithoutDeadlineUsesClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(hangUntilCancel))
	t.Cleanup(srv.Close)

	c := NewVLLMClient(srv.URL, 200*time.Millisecond)
	started := time.Now()
	_, err := c.ForwardRequest(context.Background(), "/v1/chat/completions", []byte(`{"stream":true}`))
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("stalled stream ran %s, want the client timeout", elapsed)
	}
}

// A stream whose caller already set a deadline is not cut off by the short
// client timeout. The platform timeout is what bounds it.
func TestStreamDeadlineOutlivesClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(hangUntilCancel))
	t.Cleanup(srv.Close)

	c := NewVLLMClient(srv.URL, 200*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	_, err := c.ForwardRequest(ctx, "/v1/chat/completions", []byte(`{"stream": true}`))
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected the deadline to fire")
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("client timeout won: %s", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("deadline did not stop the stream: %s", elapsed)
	}
}
