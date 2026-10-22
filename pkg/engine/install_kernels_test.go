package engine

import "testing"

func TestNeedsFlashInferKernels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		caps []string
		want bool
	}{
		{"ada 4090", []string{"8.9"}, false},
		{"ampere 3090", []string{"8.6"}, false},
		{"hopper h100", []string{"9.0"}, false},
		{"blackwell 5090", []string{"12.0"}, true},
		{"rtx pro 6000", []string{"12.0"}, true},
		{"datacenter b200", []string{"10.0"}, true},
		{"mixed ada and blackwell", []string{"8.9", "12.0"}, true},
		{"empty", nil, false},
		{"garbage", []string{"nope"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := needsFlashInferKernels(tc.caps); got != tc.want {
				t.Fatalf("needsFlashInferKernels(%v) = %v, want %v", tc.caps, got, tc.want)
			}
		})
	}
}

func TestParseComputeCap(t *testing.T) {
	t.Parallel()
	major, minor, ok := parseComputeCap(" 12.0 ")
	if !ok || major != 12 || minor != 0 {
		t.Fatalf("parse 12.0 = %d.%d ok=%v", major, minor, ok)
	}
	major, _, ok = parseComputeCap("8")
	if !ok || major != 8 {
		t.Fatalf("parse 8 = %d ok=%v", major, ok)
	}
}
