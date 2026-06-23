package usermsg

import (
	"errors"
	"strings"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
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

// ApprovalConsole returns a label and ANSI color for marketplace approval status.
func ApprovalConsole(status string) (label, color string) {
	switch status {
	case string(verify.StatusVerified):
		return "approved", "\033[1;32m"
	case string(verify.StatusUnverified):
		return "not on allowlist", "\033[1;33m"
	case string(verify.StatusFailed):
		return "failed verification", "\033[1;31m"
	case string(verify.StatusStale):
		return "stale (re-checking)", "\033[1;33m"
	case string(verify.StatusPending):
		return "pending", "\033[1;33m"
	default:
		if status == "" {
			return "unknown", "\033[0m"
		}
		return status, "\033[0m"
	}
}
