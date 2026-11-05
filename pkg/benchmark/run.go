package benchmark

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	outputTokens = 256
	promptWords  = 2000
)

// Levels tried in order. The first failure stops the run.
var Levels = []int{1, 2, 4, 8, 16}

// RunLevel fires n concurrent streaming chats at the local engine and reports
// how the slowest stream behaved.
func RunLevel(ctx context.Context, client *http.Client, baseURL, model string, n int) LevelResult {
	type one struct {
		ttft   time.Duration
		decode float64
		tokens int
		err    error
	}
	out := make([]one, n)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i] = streamOnce(ctx, client, baseURL, model)
		}(i)
	}
	wg.Wait()
	wall := time.Since(start)
	res := LevelResult{N: n}
	var total int
	for _, o := range out {
		if o.err != nil {
			res.Errors++
			continue
		}
		if o.ttft > res.P95TTFT {
			res.P95TTFT = o.ttft
		}
		if res.MinDecodeTPS == 0 || o.decode < res.MinDecodeTPS {
			res.MinDecodeTPS = o.decode
		}
		total += o.tokens
	}
	if wall > 0 {
		res.AggregateTPS = float64(total) / wall.Seconds()
	}
	return res
}

func streamOnce(ctx context.Context, client *http.Client, baseURL, model string) (one struct {
	ttft   time.Duration
	decode float64
	tokens int
	err    error
}) {
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{{
			"role": "user", "content": prompt(),
		}},
		"max_tokens":  outputTokens,
		"temperature": 0,
		"stream":      true,
		"ignore_eos":  true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		one.err = err
		return
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		one.err = err
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		one.err = fmt.Errorf("status %d", resp.StatusCode)
		return
	}
	sc := bufio.NewScanner(resp.Body)
	var first time.Time
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		if !strings.Contains(data, `"content"`) {
			continue
		}
		one.tokens++
		if first.IsZero() {
			first = time.Now()
			one.ttft = first.Sub(start)
		}
	}
	if one.tokens == 0 {
		one.err = io.ErrUnexpectedEOF
		return
	}
	gen := time.Since(first)
	if gen > 0 && one.tokens > 1 {
		one.decode = float64(one.tokens-1) / gen.Seconds()
	} else {
		one.decode = float64(one.tokens)
	}
	return
}

func prompt() string {
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	return hex.EncodeToString(rnd[:]) + " " + strings.Repeat("hello ", promptWords)
}
