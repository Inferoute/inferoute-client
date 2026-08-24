package usermsg

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
)

// ProviderName returns a display name for the configured LLM backend.
func ProviderName(providerType string) string {
	switch strings.ToLower(providerType) {
	case "vllm":
		return "vLLM"
	case "ollama":
		return "Ollama"
	default:
		return providerType
	}
}

// Console maps an error to a short message for the terminal UI.
func Console(err error, providerType string) string {
	return formatLLM(err, ProviderName(providerType))
}

// HTTP maps an error to a short message for JSON API responses.
func HTTP(err error, providerType string) string {
	return formatLLM(err, ProviderName(providerType))
}

func formatLLM(err error, name string) string {
	switch {
	case errors.Is(err, llm.ErrUnreachable):
		return "Could not connect to " + name + " — is it running?"
	case errors.Is(err, llm.ErrHTTP):
		return name + " returned an error"
	default:
		return "Could not reach " + name
	}
}

// MeasurementConsole returns a short measurement summary for the terminal UI.
func MeasurementConsole(m llm.Model) string {
	var parts []string
	if m.Digest != "" {
		parts = append(parts, shortHash(m.Digest))
	} else if m.WeightFingerprint != "" {
		parts = append(parts, shortHash(m.WeightFingerprint))
	}
	if n := len(m.Files); n > 0 {
		parts = append(parts, fmt.Sprintf("%d files", n))
	}
	if m.SizeBytes > 0 {
		parts = append(parts, formatBytes(m.SizeBytes))
	}
	if len(parts) == 0 {
		return "no measurement"
	}
	return strings.Join(parts, " · ")
}

func shortHash(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}

func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
