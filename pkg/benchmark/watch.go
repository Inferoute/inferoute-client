package benchmark

import (
	"context"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

const poll = 5 * time.Second

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
	Preemptions func(ctx context.Context) int64
	Record      func(Result)
	client      *http.Client
	applied     bool
}

// Loop polls. Explicit config and llama.cpp never get here: the caller skips them.
func (w *Watcher) Loop(ctx context.Context) {
	w.client = &http.Client{Timeout: 3 * time.Minute}
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
	if w.GPUBusy != nil && w.GPUBusy() {
		logger.Warn("benchmark starting while the GPU looks busy; the result may be low")
	}
	if w.Gate != nil {
		w.Gate(true)
	}
	defer func() {
		if w.Gate != nil {
			w.Gate(false)
		}
	}()

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
	if len(levels) == 0 || levels[0].Errors > 0 {
		best = w.fallback()
		logger.Warn("benchmark failed; using the catalog recommendation", zap.Int("concurrency", best))
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
	if w.Apply != nil {
		w.Apply(res.Concurrency)
	}
	if w.Note != nil {
		w.Note("verified for " + itoa(res.Concurrency) + " concurrent")
	}
	if w.Record != nil {
		w.Record(res)
	}
	logger.Info("concurrency set", zap.Int("n", res.Concurrency))
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
