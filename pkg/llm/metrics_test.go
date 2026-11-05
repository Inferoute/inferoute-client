package llm

import "testing"

func TestParsePrometheusMetricsVLLM(t *testing.T) {
	text := `
# HELP vllm:num_requests_waiting Number of requests waiting.
vllm:num_requests_waiting{model="Qwen"} 3
vllm:kv_cache_usage_perc{model="Qwen"} 0.42
vllm:num_preemptions_total{model="Qwen"} 7
`
	got := ParsePrometheusMetrics(text)
	if got.Waiting == nil || *got.Waiting != 3 {
		t.Fatalf("waiting = %v", got.Waiting)
	}
	if got.KVCacheUsage == nil || *got.KVCacheUsage != 0.42 {
		t.Fatalf("kv = %v", got.KVCacheUsage)
	}
	if got.Preemptions == nil || *got.Preemptions != 7 {
		t.Fatalf("preemptions = %v", got.Preemptions)
	}
}

func TestParsePrometheusMetricsFallsBackAndScalesPercent(t *testing.T) {
	text := "vllm:gpu_cache_usage_perc 80\n"
	got := ParsePrometheusMetrics(text)
	if got.KVCacheUsage == nil || *got.KVCacheUsage != 0.8 {
		t.Fatalf("kv = %v", got.KVCacheUsage)
	}
	if got.Waiting != nil {
		t.Fatal("missing waiting must stay nil, not zero")
	}
}

func TestParsePrometheusMetricsLlama(t *testing.T) {
	text := "llamacpp:requests_deferred 1\nllamacpp:kv_cache_usage_ratio 0.25\n"
	got := ParsePrometheusMetrics(text)
	if got.Waiting == nil || *got.Waiting != 1 || got.KVCacheUsage == nil || *got.KVCacheUsage != 0.25 {
		t.Fatalf("%+v", got)
	}
}
