package logger

import (
	"sync"
	"testing"
)

func TestGetDefaultLoggerConcurrentInit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	SetDefaultLogger(nil)

	const n = 32
	var wg sync.WaitGroup
	got := make([]*Logger, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			got[i] = GetDefaultLogger()
		}(i)
	}
	wg.Wait()

	first := got[0]
	if first == nil {
		t.Fatal("GetDefaultLogger() = nil")
	}
	for i, l := range got[1:] {
		if l != first {
			t.Errorf("GetDefaultLogger()[%d] = %p, want %p", i+1, l, first)
		}
	}
}
