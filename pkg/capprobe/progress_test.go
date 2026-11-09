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
	want := "5/5 checked (1 unverified, retrying)"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestFingerprintChangeDropsLateResult(t *testing.T) {
	got := resetForFingerprint("A", "B", []Outcome{Supported, Supported, Unverified})
	if len(got) != probeCount {
		t.Fatal(len(got))
	}
	for _, o := range got {
		if o != Waiting {
			t.Fatal("old result survived")
		}
	}
	kept := resetForFingerprint("A", "A", []Outcome{Supported})
	if kept[0] != Supported {
		t.Fatal("same fingerprint should keep the result")
	}
}

func TestLabelResetsWhenAllWaiting(t *testing.T) {
	if Label([]Outcome{Waiting, Waiting, Waiting, Waiting, Waiting}) != "" {
		t.Fatal("waiting should not show a count")
	}
}
