package capprobe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifyStatus(t *testing.T) {
	for status, want := range map[int]Outcome{
		400: Unsupported,
		422: Unsupported,
		401: Unverified,
		403: Unverified,
		404: Unverified,
		408: Unverified,
		409: Unverified,
		429: Unverified,
		500: Unverified,
		503: Unverified,
	} {
		if got := classifyStatus(status); got != want {
			t.Errorf("status %d = %q, want %q", status, got, want)
		}
	}
}

func TestProbeAsksForARepeatableAnswer(t *testing.T) {
	body := probeBody("m", "json_schema")
	if body["temperature"] != 0 {
		t.Fatalf("temperature = %v", body["temperature"])
	}
	if n, _ := body["max_tokens"].(int); n < 1024 {
		t.Fatalf("max_tokens = %v", body["max_tokens"])
	}
}

// probeName says which of the five checks an engine request is.
func probeName(body map[string]any) string {
	switch {
	case body["tool_choice"] == "required":
		return "forced_tools"
	case body["tools"] != nil:
		return "tools"
	case body["logprobs"] == true:
		return "logprobs"
	}
	rf, _ := body["response_format"].(map[string]any)
	if rf["type"] == "json_schema" {
		return "json_schema"
	}
	return "json_object"
}

const (
	toolCallAnswer = `{"choices":[{"message":{"content":null,"tool_calls":[{"function":{"name":"get_current_weather","arguments":"{}"}}]}}]}`
	textAnswer     = `{"choices":[{"message":{"content":"It is sunny in Paris."}}]}`
)

func passingAnswer(name string) string {
	switch name {
	case "tools", "forced_tools":
		return toolCallAnswer
	case "logprobs":
		return `{"choices":[{"message":{"content":"ok"},"logprobs":{"content":[{"token":"ok"}]}}]}`
	default:
		return `{"choices":[{"message":{"content":"{\"ok\":true,\"name\":\"x\"}"}}]}`
	}
}

// fakeEngine passes every probe. tools is called for the "tools" probe with
// the number of times it has been asked, and returns the status and body.
func fakeEngine(t *testing.T, tools func(call int) (int, string)) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	toolCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := probeName(body)
		if name == "tools" && tools != nil {
			mu.Lock()
			toolCalls++
			n := toolCalls
			mu.Unlock()
			status, answer := tools(n)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(answer))
			return
		}
		_, _ = w.Write([]byte(passingAnswer(name)))
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return toolCalls
	}
}

// fakePlatform keeps the deployment row the way the API does.
type fakePlatform struct {
	mu        sync.Mutex
	dep       deployment
	registers int
	reports   []capabilityReport
}

func (p *fakePlatform) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch {
		case r.URL.Path == "/api/provider/capabilities/deployment" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(p.dep)
		case r.URL.Path == "/api/provider/capabilities/deployment":
			var req struct {
				Fingerprint string `json:"fingerprint"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			p.registers++
			p.dep = deployment{Revision: p.dep.Revision + 1, Fingerprint: req.Fingerprint}
			_ = json.NewEncoder(w).Encode(p.dep)
		case r.URL.Path == "/api/provider/capabilities":
			var rep capabilityReport
			_ = json.NewDecoder(r.Body).Decode(&rep)
			p.dep.Attempt = rep.Attempt
			p.reports = append(p.reports, rep)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (p *fakePlatform) last() (capabilityReport, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reports) == 0 {
		return capabilityReport{}, 0, p.registers
	}
	return p.reports[len(p.reports)-1], len(p.reports), p.registers
}

func runWatcher(t *testing.T, w *Watcher) {
	t.Helper()
	w.BusyWait, w.RetryWait, w.WatchWait = 5*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond
	w.Model, w.APIKey = "m", "k"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Loop(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func toolsFlag(p *fakePlatform) *bool {
	rep, _, _ := p.last()
	return rep.Tools
}

// fullReport is true once the last report carries all five flags.
func fullReport(p *fakePlatform) bool {
	rep, _, _ := p.last()
	return rep.Tools != nil && rep.ForcedTools != nil && rep.JSONObject != nil &&
		rep.JSONSchema != nil && rep.Logprobs != nil
}

// A busy engine (429) says nothing about tools. The probe is tried again.
func TestBusyEngineIsRetriedNotMarkedUnsupported(t *testing.T) {
	engine, _ := fakeEngine(t, func(call int) (int, string) {
		if call == 1 {
			return http.StatusTooManyRequests, `{"error":"busy"}`
		}
		return http.StatusOK, toolCallAnswer
	})
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
	})
	waitFor(t, "tools reported as supported", func() bool {
		v := toolsFlag(platform)
		return v != nil && *v
	})
	platform.mu.Lock()
	defer platform.mu.Unlock()
	for _, rep := range platform.reports {
		if rep.Tools != nil && !*rep.Tools {
			t.Fatal("a 429 was reported as tools unsupported")
		}
	}
}

// A model that answers in text once and then calls the tool supports tools.
func TestTextAnswerIsRetriedBeforeUnsupported(t *testing.T) {
	engine, _ := fakeEngine(t, func(call int) (int, string) {
		if call < softFailLimit {
			return http.StatusOK, textAnswer
		}
		return http.StatusOK, toolCallAnswer
	})
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
	})
	waitFor(t, "tools reported as supported", func() bool {
		v := toolsFlag(platform)
		return v != nil && *v
	})
}

// A model that never calls the tool is unsupported after softFailLimit tries.
func TestRepeatedTextAnswerIsUnsupported(t *testing.T) {
	engine, calls := fakeEngine(t, func(int) (int, string) { return http.StatusOK, textAnswer })
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
	})
	waitFor(t, "tools reported as unsupported", func() bool {
		v := toolsFlag(platform)
		return v != nil && !*v
	})
	if n := calls(); n != softFailLimit {
		t.Fatalf("tools probe ran %d times, want %d", n, softFailLimit)
	}
}

// An engine that rejects the feature (400) is unsupported on the first try.
func TestRejectedFeatureIsUnsupportedAtOnce(t *testing.T) {
	engine, calls := fakeEngine(t, func(int) (int, string) {
		return http.StatusBadRequest, `{"error":"tools are not enabled"}`
	})
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
	})
	waitFor(t, "tools reported as unsupported", func() bool {
		v := toolsFlag(platform)
		return v != nil && !*v
	})
	if n := calls(); n != 1 {
		t.Fatalf("tools probe ran %d times, want 1", n)
	}
}

// The watcher keeps running after a full report. A changed deployment is
// registered and probed again.
func TestChangedDeploymentIsProbedAgain(t *testing.T) {
	var mu sync.Mutex
	fp := "old"
	toolsOn := false
	engine, _ := fakeEngine(t, func(int) (int, string) {
		mu.Lock()
		defer mu.Unlock()
		if toolsOn {
			return http.StatusOK, toolCallAnswer
		}
		return http.StatusBadRequest, `{"error":"tools are not enabled"}`
	})
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string {
			mu.Lock()
			defer mu.Unlock()
			return fp
		},
	})
	waitFor(t, "first report", func() bool {
		v := toolsFlag(platform)
		return v != nil && !*v
	})

	// The provider restarts the engine with the tool parser on.
	mu.Lock()
	fp, toolsOn = "new", true
	mu.Unlock()
	waitFor(t, "re-probe after the change", func() bool {
		rep, _, registers := platform.last()
		return registers == 2 && rep.Fingerprint == "new" && rep.Tools != nil && *rep.Tools
	})
}

// One odd fingerprint reading does not reset the reported capabilities.
func TestFingerprintBlipDoesNotReRegister(t *testing.T) {
	var mu sync.Mutex
	blip := false
	engine, _ := fakeEngine(t, nil)
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string {
			mu.Lock()
			defer mu.Unlock()
			if blip {
				blip = false
				return "fp-without-gpu"
			}
			return "fp"
		},
	})
	waitFor(t, "first report", func() bool {
		_, reports, _ := platform.last()
		return reports == 1
	})
	mu.Lock()
	blip = true
	mu.Unlock()
	waitFor(t, "the blip to be read", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return !blip
	})
	time.Sleep(50 * time.Millisecond)
	if _, reports, registers := platform.last(); registers != 1 || reports != 1 {
		t.Fatalf("registers=%d reports=%d after one odd reading", registers, reports)
	}
}

// Nothing is probed until the engine can be identified.
func TestNoProbeWithoutFingerprint(t *testing.T) {
	engine, calls := fakeEngine(t, func(int) (int, string) { return http.StatusOK, toolCallAnswer })
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "" },
	})
	time.Sleep(50 * time.Millisecond)
	if n := calls(); n != 0 {
		t.Fatalf("probed %d times with no fingerprint", n)
	}
}

// Probes wait while the server is not idle (a request in flight, or the benchmark).
func TestNoProbeWhileBusy(t *testing.T) {
	var mu sync.Mutex
	idle := false
	engine, calls := fakeEngine(t, func(int) (int, string) { return http.StatusOK, toolCallAnswer })
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
		Idle: func() bool {
			mu.Lock()
			defer mu.Unlock()
			return idle
		},
	})
	time.Sleep(50 * time.Millisecond)
	if n := calls(); n != 0 {
		t.Fatalf("probed %d times while busy", n)
	}
	mu.Lock()
	idle = true
	mu.Unlock()
	waitFor(t, "probe once idle", func() bool { return calls() > 0 })
}

// The benchmark, or a request, can take the engine in the middle of a round.
// The rest of the round waits until the engine is free, and probes that
// already finished are not run again.
func TestRoundStopsWhenEngineIsTaken(t *testing.T) {
	var mu sync.Mutex
	idle := true
	var ran []string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := probeName(body)
		mu.Lock()
		if !idle {
			t.Errorf("%s probe ran while the engine was taken", name)
		}
		ran = append(ran, name)
		if len(ran) == 1 {
			idle = false // the benchmark takes the engine during the first probe
		}
		mu.Unlock()
		_, _ = w.Write([]byte(passingAnswer(name)))
	}))
	t.Cleanup(engine.Close)
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
		Idle: func() bool {
			mu.Lock()
			defer mu.Unlock()
			return idle
		},
	})
	ranSoFar := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), ran...)
	}
	waitFor(t, "the first probe", func() bool { return len(ranSoFar()) > 0 })
	time.Sleep(50 * time.Millisecond)
	if got := ranSoFar(); len(got) != 1 {
		t.Fatalf("probes ran while the engine was taken: %v", got)
	}

	mu.Lock()
	idle = true
	mu.Unlock()
	waitFor(t, "the rest of the round", func() bool { return fullReport(platform) })
	if got := ranSoFar(); len(got) != probeCount {
		t.Fatalf("probes ran %v, want each once", got)
	}
}

// Each probe holds a slot while its request runs and gives it back after.
func TestEachProbeHoldsASlot(t *testing.T) {
	var held, claims atomic.Int32
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if n := held.Load(); n != 1 {
			t.Errorf("probe ran holding %d slots, want 1", n)
		}
		_, _ = w.Write([]byte(passingAnswer(probeName(body))))
	}))
	t.Cleanup(engine.Close)
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
		Claim: func() (func(), bool) {
			claims.Add(1)
			held.Add(1)
			return func() { held.Add(-1) }, true
		},
	})
	waitFor(t, "a full report", func() bool { return fullReport(platform) })
	if n := claims.Load(); n != probeCount {
		t.Fatalf("claimed %d slots, want one per probe (%d)", n, probeCount)
	}
	if n := held.Load(); n != 0 {
		t.Fatalf("%d slots still held after the round", n)
	}
}

// A refused claim means no probe, even when the engine looks idle.
func TestNoProbeWithoutASlot(t *testing.T) {
	var granted atomic.Bool
	engine, calls := fakeEngine(t, func(int) (int, string) { return http.StatusOK, toolCallAnswer })
	platform := &fakePlatform{}
	runWatcher(t, &Watcher{
		EngineURL:   engine.URL,
		PlatformURL: platform.server(t).URL,
		Fingerprint: func() string { return "fp" },
		Idle:        func() bool { return true },
		Claim: func() (func(), bool) {
			if !granted.Load() {
				return nil, false
			}
			return func() {}, true
		},
	})
	time.Sleep(50 * time.Millisecond)
	if n := calls(); n != 0 {
		t.Fatalf("probed %d times without a slot", n)
	}
	granted.Store(true)
	waitFor(t, "probe once a slot is granted", func() bool { return calls() > 0 })
}
