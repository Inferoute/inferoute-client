package llm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// EngineCapacity is what the local engine's /metrics page says.
// Nil fields mean the engine did not expose that metric.
type EngineCapacity struct {
	Waiting      *int32
	KVCacheUsage *float64
	Preemptions  *int64
}

// EngineCapacity GETs /metrics and parses the gauges Inferoute routes on.
func (c *VLLMClient) EngineCapacity(ctx context.Context) (EngineCapacity, error) {
	url := fmt.Sprintf("%s/metrics", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return EngineCapacity{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return EngineCapacity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return EngineCapacity{}, fmt.Errorf("metrics: %w", wrapHTTPStatusErr(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return EngineCapacity{}, err
	}
	return ParsePrometheusMetrics(string(body)), nil
}

// ParsePrometheusMetrics reads vLLM and llama.cpp gauges. The first sample of
// a name wins. A missing name stays nil, which is not the same as zero.
func ParsePrometheusMetrics(text string) EngineCapacity {
	var out EngineCapacity
	var kvFallback *float64
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := promSample(line)
		if !ok {
			continue
		}
		switch name {
		case "vllm:num_requests_waiting", "llamacpp:requests_deferred":
			if out.Waiting == nil {
				v := int32(value)
				out.Waiting = &v
			}
		case "vllm:kv_cache_usage_perc", "llamacpp:kv_cache_usage_ratio":
			if out.KVCacheUsage == nil {
				v := normalizeKV(value)
				out.KVCacheUsage = &v
			}
		case "vllm:gpu_cache_usage_perc":
			if kvFallback == nil {
				v := normalizeKV(value)
				kvFallback = &v
			}
		case "vllm:num_preemptions_total":
			if out.Preemptions == nil {
				v := int64(value)
				out.Preemptions = &v
			}
		}
	}
	if out.KVCacheUsage == nil {
		out.KVCacheUsage = kvFallback
	}
	return out
}

func promSample(line string) (string, float64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}
	name := fields[0]
	if i := strings.IndexByte(name, '{'); i >= 0 {
		name = name[:i]
	}
	value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		return "", 0, false
	}
	return name, value, true
}

// Older vLLM reported cache usage as 0-100. Current gauges are 0-1.
func normalizeKV(v float64) float64 {
	if v > 1 {
		return v / 100
	}
	return v
}
