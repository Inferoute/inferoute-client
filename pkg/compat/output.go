package compat

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Report is the stable JSON document emitted by `inferoute-client compatibility --json`.
type Report struct {
	Hardware Hardware      `json:"hardware"`
	Models   []ModelResult `json:"models"`
	Summary  Summary       `json:"summary"`
}

// Summary counts models per fit status.
type Summary struct {
	RunsWell int `json:"runs_well"`
	Fits     int `json:"fits"`
	Tight    int `json:"tight"`
	TooLarge int `json:"too_large"`
	Unknown  int `json:"unknown"`
	Total    int `json:"total"`
}

// BuildReport scores and sorts models for output.
func BuildReport(hw *Hardware, results []ModelResult, showTooLarge bool) Report {
	filtered := make([]ModelResult, 0, len(results))
	for _, r := range results {
		if !showTooLarge && r.Status == StatusTooLarge {
			continue
		}
		filtered = append(filtered, r)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		ri, rj := StatusRank(filtered[i].Status), StatusRank(filtered[j].Status)
		if ri != rj {
			return ri < rj
		}
		if filtered[i].ServiceType != filtered[j].ServiceType {
			return filtered[i].ServiceType < filtered[j].ServiceType
		}
		return filtered[i].Alias < filtered[j].Alias
	})

	sum := Summary{Total: len(filtered)}
	for _, r := range results {
		switch r.Status {
		case StatusRunsWell:
			sum.RunsWell++
		case StatusFits:
			sum.Fits++
		case StatusTight:
			sum.Tight++
		case StatusTooLarge:
			sum.TooLarge++
		default:
			sum.Unknown++
		}
	}
	// Summary always reflects the full scored set (before showTooLarge filter).
	sum.Total = len(results)

	hwCopy := Hardware{}
	if hw != nil {
		hwCopy = *hw
	}
	return Report{Hardware: hwCopy, Models: filtered, Summary: sum}
}

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// WriteTable writes a human-readable hardware + models table.
func WriteTable(w io.Writer, report Report) error {
	hw := report.Hardware
	fmt.Fprintln(w, "Hardware")
	fmt.Fprintf(w, "  OS/Arch:        %s/%s\n", hw.OS, hw.Arch)
	fmt.Fprintf(w, "  GPU:            %s\n", emptyDash(hw.ProductName))
	if hw.GPUCount > 0 {
		fmt.Fprintf(w, "  GPU count:      %d\n", hw.GPUCount)
	}
	if hw.DriverVersion != "" {
		fmt.Fprintf(w, "  Driver:         %s\n", hw.DriverVersion)
	}
	if hw.CUDAVersion != "" && hw.CUDAVersion != "N/A" {
		fmt.Fprintf(w, "  CUDA:           %s\n", hw.CUDAVersion)
	}
	fmt.Fprintf(w, "  Memory kind:    %s\n", hw.MemoryKind)
	if hw.UnifiedMemory {
		fmt.Fprintln(w, "  Unified memory: yes (Apple Silicon)")
	}
	if hw.SystemRAMBytes > 0 {
		fmt.Fprintf(w, "  System RAM:     %s\n", formatBytes(hw.SystemRAMBytes))
	}
	if hw.MemoryTotalBytes > 0 {
		fmt.Fprintf(w, "  Memory total:   %s\n", formatBytes(hw.MemoryTotalBytes))
	}
	if hw.MemoryFreeBytes > 0 {
		fmt.Fprintf(w, "  Memory free:    %s\n", formatBytes(hw.MemoryFreeBytes))
	}
	fmt.Fprintf(w, "  Usable (score): %s\n", formatBytes(hw.UsableBytes))
	for _, warn := range hw.Warnings {
		fmt.Fprintf(w, "  warning: %s\n", warn)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Summary: %d runs_well, %d fits, %d tight, %d too_large, %d unknown (%d total)\n\n",
		report.Summary.RunsWell, report.Summary.Fits, report.Summary.Tight,
		report.Summary.TooLarge, report.Summary.Unknown, report.Summary.Total)

	if len(report.Models) == 0 {
		fmt.Fprintln(w, "No models to display.")
		return nil
	}

	var raw strings.Builder
	tw := tabwriter.NewWriter(&raw, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tSTATUS\tSERVICE\tMODEL\tSIZE\tREQUIRED")
	for i, m := range report.Models {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			m.Status,
			m.ServiceType,
			shortAlias(m.Alias),
			formatBytes(m.MinSizeBytes),
			formatBytes(m.RequiredBytes),
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	lines := strings.Split(strings.TrimSuffix(raw.String(), "\n"), "\n")
	for i, line := range lines {
		if i == 0 {
			fmt.Fprintln(w, line)
			continue
		}
		if usableFit(report.Models[i-1].Status) {
			fmt.Fprintf(w, "\033[1;32m%s\033[0m\n", line)
			continue
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

func usableFit(s FitStatus) bool {
	return s == StatusRunsWell || s == StatusFits || s == StatusTight
}

func shortAlias(alias string) string {
	if len(alias) <= 48 {
		return alias
	}
	return alias[:45] + "..."
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
