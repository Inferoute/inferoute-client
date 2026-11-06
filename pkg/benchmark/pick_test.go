package benchmark

import (
	"testing"
	"time"
)

func lvl(n, errors, pre int, ttft time.Duration, decode, agg float64) LevelResult {
	return LevelResult{N: n, Errors: errors, Preemptions: pre, P95TTFT: ttft, MinDecodeTPS: decode, AggregateTPS: agg}
}

func TestPick(t *testing.T) {
	ok := 500 * time.Millisecond
	cases := []struct {
		name string
		in   []LevelResult
		want int
	}{
		{
			name: "keeps rising",
			in: []LevelResult{
				lvl(1, 0, 0, ok, 40, 40),
				lvl(2, 0, 0, ok, 30, 55),
				lvl(4, 0, 0, ok, 22, 70),
			},
			want: 4,
		},
		{
			name: "stops when throughput flattens",
			in: []LevelResult{
				lvl(1, 0, 0, ok, 40, 40),
				lvl(2, 0, 0, ok, 30, 42),
			},
			want: 1,
		},
		{
			name: "stops on preemption",
			in: []LevelResult{
				lvl(1, 0, 0, ok, 40, 40),
				lvl(2, 0, 1, ok, 30, 60),
			},
			want: 1,
		},
		{
			name: "stops when a stream gets too slow",
			in: []LevelResult{
				lvl(1, 0, 0, ok, 40, 40),
				lvl(2, 0, 0, ok, 10, 80),
			},
			want: 1,
		},
		{
			name: "empty",
			in:   nil,
			want: 0,
		},
		{
			name: "n=1 errors",
			in:   []LevelResult{lvl(1, 1, 0, ok, 0, 0)},
			want: 0,
		},
		{
			name: "n=1 slow TTFT",
			in:   []LevelResult{lvl(1, 0, 0, 4*time.Second, 40, 40)},
			want: 0,
		},
		{
			name: "n=1 slow decode",
			in:   []LevelResult{lvl(1, 0, 0, ok, 10, 10)},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Pick(tc.in); got != tc.want {
				t.Fatalf("Pick = %d, want %d", got, tc.want)
			}
		})
	}
}
