package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/benchmark"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/capprobe"
)

// passesEveryProbe is an engine answer that passes all five capability checks.
const passesEveryProbe = `{"choices":[{"message":{"content":"{\"ok\":true,\"name\":\"x\"}",` +
	`"tool_calls":[{"function":{"name":"get_current_weather","arguments":"{}"}}]},` +
	`"logprobs":{"content":[{"token":"ok"}]}}]}`

// The capability probes and the concurrency benchmark share one engine, wired
// as cmd/main.go wires them. A benchmark that starts during a probe waits for
// it, and the rest of the probe round waits for the benchmark.
func TestProbesAndBenchmarkTakeTurns(t *testing.T) {
	s := &Server{}
	s.maxInflight.Store(4)

	var probing, benching atomic.Int32 // requests on the engine right now
	var probes, benchChats, overlaps atomic.Int32
	firstProbe := make(chan struct{})
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true { // a benchmark chat
			benching.Add(1)
			defer benching.Add(-1)
			benchChats.Add(1)
			if probing.Load() > 0 {
				overlaps.Add(1)
			}
			// Probes that ignore the gate would arrive now.
			time.Sleep(100 * time.Millisecond)
			// A failed first level ends the run on the fallback.
			http.Error(w, "fail", http.StatusInternalServerError)
			return
		}
		probing.Add(1)
		defer probing.Add(-1)
		if benching.Load() > 0 {
			overlaps.Add(1)
		}
		if probes.Add(1) == 1 {
			// The benchmark starts during this probe. A benchmark that does
			// not wait would send its chats in the next 100ms.
			close(firstProbe)
			deadline := time.Now().Add(2 * time.Second)
			for !s.benchmarking.Load() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond)
		}
		_, _ = w.Write([]byte(passesEveryProbe))
	}))
	t.Cleanup(engine.Close)

	ctx, cancel := context.WithCancel(context.Background())
	var loops sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		loops.Wait()
	})
	run := func(loop func(context.Context)) {
		loops.Add(1)
		go func() {
			defer loops.Done()
			loop(ctx)
		}()
	}

	run((&capprobe.Watcher{
		EngineURL:   engine.URL,
		Model:       "m",
		Fingerprint: func() string { return "fp" },
		Idle: func() bool {
			inflight, _, accepting := s.SlotSnapshot()
			return inflight == 0 && accepting
		},
		Claim:     s.ClaimIdleSlot,
		BusyWait:  5 * time.Millisecond,
		RetryWait: 5 * time.Millisecond,
		WatchWait: 5 * time.Millisecond,
	}).Loop)
	select {
	case <-firstProbe:
	case <-time.After(5 * time.Second):
		t.Fatal("no probe reached the engine")
	}

	var applied atomic.Int32
	run((&benchmark.Watcher{
		ConfigPath:  filepath.Join(t.TempDir(), "config.yaml"),
		EngineURL:   engine.URL,
		Model:       "m",
		Ready:       func() bool { return true },
		Fingerprint: func() string { return "fp" },
		Apply: func(n int) {
			s.SetMaxInflight(int32(n))
			applied.Store(int32(n))
		},
		Gate: s.SetBenchmarking,
		Inflight: func() int32 {
			n, _, _ := s.SlotSnapshot()
			return n
		},
	}).Loop)

	deadline := time.Now().Add(5 * time.Second)
	for applied.Load() == 0 || probes.Load() < 5 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: applied=%d probes=%d", applied.Load(), probes.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := overlaps.Load(); n != 0 {
		t.Fatalf("probes and the benchmark used the engine together %d times", n)
	}
	if benchChats.Load() == 0 {
		t.Fatal("the benchmark never reached the engine")
	}
	if n := probes.Load(); n != 5 {
		t.Fatalf("probes ran %d times, want each of the five once", n)
	}
}
