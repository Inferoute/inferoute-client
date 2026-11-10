package capprobe

import "fmt"

const probeCount = 5

// Names are the five checks, in order.
var Names = []string{"tools", "forced_tools", "json_object", "json_schema", "logprobs"}

// Outcome is one probe result. Empty means not checked yet.
type Outcome string

const (
	Waiting     Outcome = ""
	Supported   Outcome = "supported"
	Unsupported Outcome = "unsupported"
	Unverified  Outcome = "unverified"
)

// Label is the status line. Checked counts supported, unsupported, and unverified.
func Label(outcomes []Outcome) string {
	if len(outcomes) > probeCount {
		outcomes = outcomes[:probeCount]
	}
	checked := 0
	unverified := 0
	for _, o := range outcomes {
		if o == Waiting {
			continue
		}
		checked++
		if o == Unverified {
			unverified++
		}
	}
	if checked == 0 {
		return ""
	}
	line := fmt.Sprintf("testing - %d/%d checked (still serving requests)", checked, probeCount)
	if checked == probeCount {
		line = "completed"
		if unverified > 0 {
			line = fmt.Sprintf("completed (%d unverified, retrying)", unverified)
		}
	}
	return line
}
