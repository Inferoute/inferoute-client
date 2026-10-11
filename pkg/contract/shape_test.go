package contract_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/cloudflare"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/pricing"
)

// These fixtures are the request bodies copied into inferoute-node
// tests/contract/testdata/client/<version>/ on each client release.
// A mismatch means the node contract suite is now protecting the wrong payload.
func TestRequestShapes(t *testing.T) {
	cases := []struct {
		file string
		body any
	}{
		{"health.request.json", health.HealthReport{
			Object: "list",
			Data: []llm.Model{{
				ID: "Qwen/Qwen2.5-7B-Instruct", Object: "model", Created: 1710000000, OwnedBy: "inferoute",
				SizeBytes: 100, WeightFingerprint: "fp", VerificationStatus: "verified",
				MaxModelLen: 8192,
			}},
			GPU: &gpu.GPUInfo{
				ProductName: "Example GPU", DriverVersion: "550.0", CUDAVersion: "12.4",
				GPUCount: 1, UUID: "GPU-example", MemoryTotal: 24576, MemoryUsed: 1024, MemoryFree: 23552,
			},
			Cloudflare: map[string]interface{}{
				"url": "https://inferoute-cluster1.example.com", "public_ip": "203.0.113.10",
				"location": "London, England, United Kingdom",
			},
			ProviderType: "vllm",
		}},
		{"models.request.json", pricing.RegisterModelRequest{
			ModelName: "Qwen/Qwen2.5-7B-Instruct", ServiceType: "vllm",
			InputPriceTokens: 0.0000005, OutputPriceTokens: 0.000001,
		}},
		{"get-prices.request.json", pricing.GetPricesRequest{Models: []string{"Qwen/Qwen2.5-7B-Instruct"}}},
		{"tunnel-request.request.json", cloudflare.TunnelRequest{ServiceURL: "http://127.0.0.1:8080"}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if !jsonEqual(got, want) {
				t.Fatalf("payload drifted from %s\ngot  %s\nwant %s", tc.file, got, want)
			}
		})
	}
}

func jsonEqual(got, want []byte) bool {
	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal(want, &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}
