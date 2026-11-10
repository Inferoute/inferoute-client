package capprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

const (
	// softFailLimit is how many times a probe can get a 200 that fails the
	// check before the capability is stored as unsupported. Models do not
	// answer the same way every time.
	softFailLimit = 3

	probeTimeout = 2 * time.Minute
)

// Watcher runs the five probes while the engine is idle, reports them, and
// keeps watching: a changed deployment is probed again.
type Watcher struct {
	EngineURL   string
	Model       string
	PlatformURL string
	APIKey      string
	// Fingerprint identifies the deployment. Empty means the engine is not
	// answering yet, so nothing is probed.
	Fingerprint func() string
	Idle        func() bool
	Note        func(string)
	Client      *http.Client

	// Waits. Zero uses the defaults; tests shorten them.
	BusyWait  time.Duration
	RetryWait time.Duration
	WatchWait time.Duration

	softFails [probeCount]int
}

func (w *Watcher) Loop(ctx context.Context) {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: probeTimeout + 10*time.Second}
	}
	busyWait := orDefault(w.BusyWait, 5*time.Second)
	retryWait := orDefault(w.RetryWait, time.Minute)
	watchWait := orDefault(w.WatchWait, time.Minute)

	outcomes := make([]Outcome, probeCount)
	seen := ""      // deployment the outcomes belong to
	candidate := "" // a different fingerprint seen once, not yet trusted
	dirty := false  // results not yet reported
	for {
		if ctx.Err() != nil {
			return
		}
		if w.Idle != nil && !w.Idle() {
			if !sleep(ctx, busyWait) {
				return
			}
			continue
		}
		fp := ""
		if w.Fingerprint != nil {
			fp = w.Fingerprint()
		}
		if fp == "" {
			if !sleep(ctx, busyWait) {
				return
			}
			continue
		}
		if fp != seen {
			// One odd reading (a GPU query that failed, say) must not wipe
			// the reported capabilities. Act on a change seen twice.
			if seen != "" && fp != candidate {
				candidate = fp
				if !sleep(ctx, busyWait) {
					return
				}
				continue
			}
			outcomes = make([]Outcome, probeCount)
			w.softFails = [probeCount]int{}
			seen = fp
		}
		candidate = ""

		paused := false
		for i, name := range Names {
			if outcomes[i] != Waiting {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			// Idle is not stable across this loop. The startup benchmark can
			// close the gate while a fingerprint is being read, and a probe
			// started in that window runs beside the concurrency test.
			if w.Idle != nil && !w.Idle() {
				paused = true
				break
			}
			outcomes[i] = w.settle(i, probe(ctx, w.Client, w.EngineURL, w.Model, name))
			dirty = true
			if w.Note != nil {
				w.Note(Label(outcomes))
			}
		}
		if dirty {
			if err := w.publish(ctx, seen, outcomes); err != nil {
				logger.Warn("capability report failed", zap.Error(err))
				if !sleep(ctx, retryWait) {
					return
				}
				continue
			}
			dirty = false
		}
		if paused {
			if !sleep(ctx, busyWait) {
				return
			}
			for i, o := range outcomes {
				if o == Unverified {
					outcomes[i] = Waiting
				}
			}
			continue
		}
		if needsRetry(outcomes) {
			if !sleep(ctx, retryWait) {
				return
			}
			for i, o := range outcomes {
				if o == Unverified {
					outcomes[i] = Waiting
				}
			}
			continue
		}
		// Everything is settled. Stay up and look for a changed deployment.
		if !sleep(ctx, watchWait) {
			return
		}
	}
}

// settle turns one probe result into the stored outcome. A 200 that failed the
// check is retried until it has failed softFailLimit times.
func (w *Watcher) settle(i int, res probeResult) Outcome {
	if res.outcome == Supported {
		w.softFails[i] = 0
		return Supported
	}
	if res.outcome == Unsupported && res.soft {
		w.softFails[i]++
		if w.softFails[i] < softFailLimit {
			return Unverified
		}
	}
	return res.outcome
}

func orDefault(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

// sleep waits for d. It returns false when ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func needsRetry(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o == Unverified || o == Waiting {
			return true
		}
	}
	return false
}

type deployment struct {
	Revision    int64  `json:"revision"`
	Fingerprint string `json:"fingerprint"`
	Attempt     int64  `json:"attempt"`
}

type capabilityReport struct {
	Fingerprint string `json:"fingerprint"`
	Revision    int64  `json:"revision"`
	Attempt     int64  `json:"attempt"`
	Tools       *bool  `json:"tools,omitempty"`
	ForcedTools *bool  `json:"forced_tools,omitempty"`
	JSONObject  *bool  `json:"json_object,omitempty"`
	JSONSchema  *bool  `json:"json_schema,omitempty"`
	Logprobs    *bool  `json:"logprobs,omitempty"`
}

func (w *Watcher) publish(ctx context.Context, fp string, outcomes []Outcome) error {
	if w.PlatformURL == "" || w.APIKey == "" {
		return nil
	}
	if fp == "" {
		return fmt.Errorf("empty fingerprint")
	}
	return report(ctx, w.Client, w.PlatformURL, w.APIKey, fp, outcomes)
}

// report registers the deployment, then records this attempt.
// The API rejects a record whose revision is 0 or whose attempt is not newer.
func report(ctx context.Context, client *http.Client, base, key, fp string, outcomes []Outcome) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var dep deployment
	code, raw, err := callJSON(ctx, client, http.MethodGet, base+"/api/provider/capabilities/deployment", key, nil, &dep)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("GET capabilities/deployment: %d %s", code, bytes.TrimSpace(raw))
	}
	if dep.Fingerprint != fp {
		code, raw, err = callJSON(ctx, client, http.MethodPost, base+"/api/provider/capabilities/deployment", key, map[string]any{
			"fingerprint":   fp,
			"prev_revision": dep.Revision,
		}, &dep)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("POST capabilities/deployment: %d %s", code, bytes.TrimSpace(raw))
		}
	}
	body := capabilityReport{
		Fingerprint: fp,
		Revision:    dep.Revision,
		Attempt:     dep.Attempt + 1,
		Tools:       outcomeBool(flagAt(outcomes, 0)),
		ForcedTools: outcomeBool(flagAt(outcomes, 1)),
		JSONObject:  outcomeBool(flagAt(outcomes, 2)),
		JSONSchema:  outcomeBool(flagAt(outcomes, 3)),
		Logprobs:    outcomeBool(flagAt(outcomes, 4)),
	}
	code, raw, err = callJSON(ctx, client, http.MethodPost, base+"/api/provider/capabilities", key, body, nil)
	if err != nil {
		return err
	}
	if code != http.StatusNoContent {
		return fmt.Errorf("POST capabilities: %d %s", code, bytes.TrimSpace(raw))
	}
	return nil
}

func flagAt(outcomes []Outcome, i int) Outcome {
	if i >= len(outcomes) {
		return Waiting
	}
	return outcomes[i]
}

func outcomeBool(o Outcome) *bool {
	switch o {
	case Supported:
		v := true
		return &v
	case Unsupported:
		v := false
		return &v
	default:
		return nil
	}
}

func callJSON(ctx context.Context, client *http.Client, method, url, key string, in, out any) (int, []byte, error) {
	var rdr io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if out != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, raw, err
		}
	}
	return resp.StatusCode, raw, nil
}

func probeBody(model, name string) map[string]any {
	body := map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": "Reply with ok."}},
		// Room for a reasoning model to think and still answer.
		"max_tokens":  1024,
		"temperature": 0,
		"stream":      false,
	}
	switch name {
	case "tools", "forced_tools":
		// An empty ping tool with "Reply with ok" is answered in text, so auto was stored as unsupported.
		body["messages"] = []map[string]string{{"role": "user", "content": "What is the weather in Paris right now? Use the get_current_weather tool."}}
		body["tools"] = []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_current_weather",
				"description": "Get the current weather in a given city",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string", "description": "City name"},
					},
					"required": []string{"city"},
				},
			},
		}}
		body["tool_choice"] = "auto"
		if name == "forced_tools" {
			body["tool_choice"] = "required"
		}
	case "json_object":
		body["response_format"] = map[string]any{"type": "json_object"}
	case "json_schema":
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "ping",
				"schema": map[string]any{
					"type":     "object",
					"required": []string{"ok", "name"},
					"properties": map[string]any{
						"ok":   map[string]any{"type": "boolean"},
						"name": map[string]any{"type": "string"},
					},
				},
			},
		}
	case "logprobs":
		body["logprobs"] = true
		body["top_logprobs"] = 1
	}
	return body
}

// probeResult is one probe's outcome. soft marks an Unsupported that came from
// a 200 whose answer failed the check, which can be the model and not the engine.
type probeResult struct {
	outcome Outcome
	soft    bool
}

// classifyStatus maps a non-2xx engine status. Only 400 and 422 mean the engine
// does not accept the feature. Busy, auth, not-found, and server errors say
// nothing about the feature, so they are tried again.
func classifyStatus(status int) Outcome {
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		return Unsupported
	}
	return Unverified
}

func probe(ctx context.Context, client *http.Client, base, model, name string) probeResult {
	raw, _ := json.Marshal(probeBody(model, name))
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return probeResult{outcome: Unverified}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return probeResult{outcome: Unverified}
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return probeResult{outcome: classifyStatus(resp.StatusCode)}
	}
	if probePassed(name, rawBody) {
		return probeResult{outcome: Supported}
	}
	return probeResult{outcome: Unsupported, soft: true}
}

func probePassed(name string, raw []byte) bool {
	var resp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			Logprobs *struct {
				Content []any `json:"content"`
			} `json:"logprobs"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &resp) != nil || len(resp.Choices) == 0 {
		return false
	}
	choice := resp.Choices[0]
	switch name {
	case "tools", "forced_tools":
		return len(choice.Message.ToolCalls) > 0 && choice.Message.ToolCalls[0].Function.Name != ""
	case "json_object":
		var v any
		return json.Unmarshal([]byte(choice.Message.Content), &v) == nil
	case "json_schema":
		var v map[string]any
		if json.Unmarshal([]byte(choice.Message.Content), &v) != nil {
			return false
		}
		_, a := v["ok"]
		_, b := v["name"]
		return a && b
	case "logprobs":
		return choice.Logprobs != nil && len(choice.Logprobs.Content) > 0
	default:
		return false
	}
}
