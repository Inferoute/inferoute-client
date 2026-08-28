package main

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestIsFatalServerErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "graceful shutdown", err: http.ErrServerClosed, want: false},
		{name: "wrapped shutdown", err: fmt.Errorf("serve: %w", http.ErrServerClosed), want: false},
		{name: "tunnel start failure", err: errors.New("failed to start tunnel: cloudflared process died during startup"), want: true},
		{name: "listen failure", err: errors.New("listen tcp :8080: bind: address already in use"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFatalServerErr(tt.err); got != tt.want {
				t.Fatalf("isFatalServerErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
