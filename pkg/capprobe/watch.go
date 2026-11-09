package capprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
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
		for i, name := range Names {
			if ctx.Err() != nil {
				return
			}
			if w.Fingerprint != nil {
				outcomes = resetForFingerprint(fp, w.Fingerprint(), outcomes)
				if w.Fingerprint() != fp {
					break
				}
			}
			outcomes[i] = probe(ctx, client, w.EngineURL, w.Model, name)
			if w.Note != nil {
				w.Note(Label(outcomes))
			}
		}
		w.publish(ctx, outcomes)
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

func needsRetry(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o == Unverified || o == Waiting {
			return true
		}
	}
	return false
}

func (w *Watcher) publish(ctx context.Context, outcomes []Outcome) {
	if w.PlatformURL == "" || w.APIKey == "" || w.Fingerprint == nil {
		return
	}
	fp := w.Fingerprint()
	if fp == "" {
		return
	}
	body := map[string]any{"fingerprint": fp}
	set := func(key string, o Outcome) {
		switch o {
		case Supported:
			body[key] = true
		case Unsupported:
			body[key] = false
		}
	}
	keys := []string{"tools", "forced_tools", "json_object", "json_schema", "logprobs"}
	for i, key := range keys {
		if i < len(outcomes) {
			set(key, outcomes[i])
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, w.PlatformURL+"/api/provider/capabilities", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.APIKey)
	resp, err := w.Client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
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
