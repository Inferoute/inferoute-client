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

// Watcher runs the five probes while the GPU is idle and reports the label.
type Watcher struct {
	EngineURL   string
	Model       string
	PlatformURL string
	APIKey      string
	Fingerprint func() string
	Idle        func() bool
	Note        func(string)
	Client      *http.Client
}

func (w *Watcher) Loop(ctx context.Context) {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 60 * time.Second}
	}
	client := w.Client
	outcomes := make([]Outcome, probeCount)
	var seen string
	for {
		if ctx.Err() != nil {
			return
		}
		if w.Idle != nil && !w.Idle() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		fp := ""
		if w.Fingerprint != nil {
			fp = w.Fingerprint()
		}
		outcomes = resetForFingerprint(seen, fp, outcomes)
		seen = fp
		for i, name := range Names {
			if outcomes[i] != Waiting {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			if w.Fingerprint != nil && w.Fingerprint() != fp {
				outcomes = resetForFingerprint(fp, w.Fingerprint(), outcomes)
				seen = w.Fingerprint()
				break
			}
			outcomes[i] = probe(ctx, client, w.EngineURL, w.Model, name)
			if w.Note != nil {
				w.Note(Label(outcomes))
			}
		}
		if !anyChecked(outcomes) {
			continue
		}
		if err := w.publish(ctx, fp, outcomes); err != nil {
			logger.Warn("capability report failed", zap.Error(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
			continue
		}
		if !needsRetry(outcomes) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
		for i, o := range outcomes {
			if o == Unverified {
				outcomes[i] = Waiting
			}
		}
	}
}

func anyChecked(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o != Waiting {
			return true
		}
	}
	return false
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

func probe(ctx context.Context, client *http.Client, base, model, name string) Outcome {
	body := map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "Reply with ok."}},
		"max_tokens": 256,
		"stream":     false,
	}
	switch name {
	case "tools", "forced_tools":
		body["tools"] = []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "ping",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}}
		if name == "forced_tools" {
			body["tool_choice"] = "required"
		} else {
			body["tool_choice"] = "auto"
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
	raw, _ := json.Marshal(body)
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Unverified
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Unverified
		}
		return Unverified
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return Unsupported
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Unverified
	}
	if probePassed(name, rawBody) {
		return Supported
	}
	return Unsupported
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
