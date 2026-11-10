package capprobe

import "testing"

func TestLabelCountsCheckedIncludingUnverified(t *testing.T) {
	got := Label([]Outcome{Supported, Unverified, Waiting, Waiting, Waiting})
	want := "testing - 2/5 checked (still serving requests)"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestLabelDoneNamesUnverified(t *testing.T) {
	got := Label([]Outcome{Supported, Supported, Supported, Supported, Unverified})
	want := "completed (1 unverified, retrying)"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestLabelDoneSaysCompleted(t *testing.T) {
	got := Label([]Outcome{Supported, Unsupported, Supported, Supported, Supported})
	if got != "completed" {
		t.Fatalf("got %q", got)
	}
}

func TestLabelResetsWhenAllWaiting(t *testing.T) {
	if Label([]Outcome{Waiting, Waiting, Waiting, Waiting, Waiting}) != "" {
		t.Fatal("waiting should not show a count")
	}
}
