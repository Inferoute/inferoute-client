package logger

import (
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestLogsStayInFiles(t *testing.T) {
	dir := t.TempDir()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })

	l, err := New(&Config{
		Level:      "info",
		LogDir:     dir,
		MaxSize:    1,
		MaxBackups: 1,
		MaxAge:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello")
	l.Error("boom")
	_ = l.Sync()
	w.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("stdout = %q", out)
	}

	mainLog, err := os.ReadFile(filepath.Join(dir, "inferoute.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainLog), "hello") || !strings.Contains(string(mainLog), "boom") {
		t.Fatalf("inferoute.log = %s", mainLog)
	}
	errLog, err := os.ReadFile(filepath.Join(dir, "error.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(errLog), "boom") || strings.Contains(string(errLog), "hello") {
		t.Fatalf("error.log = %s", errLog)
	}
}
