package benchmark

import (
	"context"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

const poll = 5 * time.Second

// drainWait is how long a benchmark waits, with the gate closed, for requests
// and capability probes that were already running. It is longer than the LLM
// forward timeout (at least 330s), so a non-streaming request always ends
// first. A longer stream defers the benchmark until that stream is gone.
// Retrying on the next poll re-closes the gate while the stream is still
// running, admits another one in the gap, and keeps inference rejected.
var drainWait = 6 * time.Minute

// Watcher measures once the engine is ready, then stays quiet until the
// engine drops or someone asks for another run.
type Watcher struct {
	ConfigPath  string
	EngineURL   string
	Model       string
	Ready       func() bool
	Fingerprint func() string
	Recommended func() int
	Apply       func(n int)
	Note        func(string)
	Gate        func(closed bool)
	GPUBusy     func() bool
	Inflight    func() int32
	Preemptions func(ctx context.Context) int64
	Record      func(Result)
	client      *http.Client
	applied     bool
	// drainPaused is set when a drain times out. The gate stays open until
	// inflight hits zero so a long stream cannot pin the node shut.
	drainPaused bool
}

// Loop polls. Explicit config and llama.cpp never get here: the caller skips them.
func (w *Watcher) Loop(ctx context.Context) {
	w.client = &http.Client{Timeout: 3 * time.Minute}
	if w.Gate != nil {
		w.Gate(true)
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	w.step(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			w.step(ctx)
		}
	}
}

func (w *Watcher) step(ctx context.Context) {
	if consumeForce(w.ConfigPath) {
		w.applied = false
		w.drainPaused = false
	}
	ready := w.Ready != nil && w.Ready()
	if !ready {
		w.applied = false
		return
	}
	if w.applied {
		return
	}
	fp := ""
	if w.Fingerprint != nil {
		fp = w.Fingerprint()
	}
	if cached, ok := load(w.ConfigPath); ok && cached.Fingerprint == fp && fp != "" {
		w.finish(cached)
		return
	}
	w.measure(ctx, fp)
}

func (w *Watcher) measure(ctx context.Context, fp string) {
	// The poll is 5s. A stream that outlasted drainWait is still holding a
	// slot, and closing the gate again lets a new long stream in during the
	// gap. Leave the gate open until this one finishes.
	if w.drainPaused && w.Inflight != nil && w.Inflight() > 0 {
		return
	}
	w.drainPaused = false
	if w.Gate != nil {
		w.Gate(true)
	}
	defer func() {
		if w.Gate != nil {
			w.Gate(false)
		}
	}()
	// The gate stops new requests. Requests already running and capability
	// probes still hold slots, so wait for them before measuring.
	if !w.waitIdle(ctx) {
		if ctx.Err() == nil {
			logger.Warn("benchmark deferred: requests still in flight", zap.Duration("waited", drainWait))
			w.drainPaused = true
		}
		return
	}
	// Our own work has finished, so a busy GPU here is another program.
	if w.GPUBusy != nil && w.GPUBusy() {
		logger.Warn("benchmark starting while the GPU looks busy; the result may be low")
	}

	var levels []LevelResult
	for i, n := range Levels {
		if w.Note != nil {
			w.Note("benchmarking (" + itoa(i+1) + "/" + itoa(len(Levels)) + ")")
		}
		before := int64(0)
		if w.Preemptions != nil {
			before = w.Preemptions(ctx)
		}
		lvl := RunLevel(ctx, w.client, w.EngineURL, w.Model, n)
		if w.Preemptions != nil {
			lvl.Preemptions = int(w.Preemptions(ctx) - before)
			if lvl.Preemptions < 0 {
				lvl.Preemptions = 0
			}
		}
		levels = append(levels, lvl)
		if Pick(levels) != n {
			break
		}
	}
	best := Pick(levels)
	if best < 1 {
		if len(levels) == 0 || levels[0].Errors > 0 {
			best = w.fallback()
			logger.Warn("benchmark failed; using the catalog recommendation", zap.Int("concurrency", best))
		} else {
			// n=1 completed but missed the quality bar (slow TTFT / decode).
			// Stay at 1; do not treat 0 as unlimited slots.
			best = 1
		}
	}
	res := Result{
		Fingerprint: fp,
		Concurrency: best,
		MeasuredAt:  time.Now().UTC(),
		Levels:      levels,
		GPUBusy:     w.GPUBusy != nil && w.GPUBusy(),
	}
	if err := save(w.ConfigPath, res); err != nil {
		logger.Warn("could not save benchmark", zap.Error(err))
	}
	w.finish(res)
}

func (w *Watcher) finish(res Result) {
	w.applied = true
	w.drainPaused = false
	if w.Apply != nil {
		w.Apply(res.Concurrency)
	}
	if w.Note != nil {
		w.Note("verified for " + itoa(res.Concurrency) + " concurrent")
	}
	if w.Record != nil {
		w.Record(res)
	}
	if w.Gate != nil {
		w.Gate(false)
	}
	logger.Info("concurrency set", zap.Int("n", res.Concurrency))
}

// waitIdle is true once nothing holds a slot: no request admitted before the
// gate closed, and no capability probe. It is false after drainWait.
func (w *Watcher) waitIdle(ctx context.Context) bool {
	if w.Inflight == nil || w.Inflight() <= 0 {
		return true
	}
	if w.Note != nil {
		w.Note("waiting for requests to finish")
	}
	deadline := time.Now().Add(drainWait)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
			if w.Inflight() <= 0 {
				return true
			}
			if time.Now().After(deadline) {
				return false
			}
		}
	}
}

func (w *Watcher) fallback() int {
	if w.Recommended != nil {
		if n := w.Recommended(); n > 0 {
			return n
		}
	}
	return 4
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
