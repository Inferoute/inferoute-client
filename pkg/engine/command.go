package engine

import (
	"fmt"
	"strings"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

// Spec is the argv used to start an engine.
type Spec struct {
	Bin  string
	Args []string
	URL  string
}

// CommandLine is a copy-pasteable shell form of Spec.
func (s Spec) CommandLine() string {
	parts := make([]string, 0, 1+len(s.Args))
	if s.Bin != "" {
		parts = append(parts, quote(s.Bin))
	}
	for _, a := range s.Args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

func quote(s string) string {
	if s == "" {
		return s
	}
	if strings.ContainsAny(s, " \t\"'") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// ServeSpec builds the start command for kind.
// modelAlias is the Inferoute catalog alias; hfRepo is the HuggingFace id (vLLM/FreeToken).
func ServeSpec(kind Kind, bin, modelAlias, hfRepo string) Spec {
	if bin == "" {
		bin = defaultBinName(kind)
	}
	spec := Spec{Bin: bin, URL: DefaultURL(kind)}
	switch kind {
	case KindOllama:
		spec.Args = []string{"serve"}
	case KindVLLM, KindVLLMMetal:
		repo := firstNonEmpty(hfRepo, modelAlias)
		spec.Args = []string{"serve", repo}
		if modelAlias != "" && modelAlias != repo {
			spec.Args = append(spec.Args, "--served-model-name", modelAlias)
		}
	case KindFreeToken:
		repo := firstNonEmpty(hfRepo, modelAlias)
		name := firstNonEmpty(modelAlias, repo)
		spec.Args = []string{"serve", "--model", repo, "--served-model-name", name, "--host", "127.0.0.1", "--port", "1919"}
	}
	return spec
}

func defaultBinName(kind Kind) string {
	if kind == KindFreeToken {
		return "ft"
	}
	if kind == KindOllama {
		return "ollama"
	}
	return "vllm"
}

// PullSpec is ollama pull for a catalog alias (strips a leading gguf/ prefix).
func PullSpec(bin, modelAlias string) Spec {
	if bin == "" {
		bin = "ollama"
	}
	return Spec{Bin: bin, Args: []string{"pull", OllamaPullName(modelAlias)}}
}

// OllamaPullName maps a catalog alias like gguf/qwen3:0.6b to the Ollama tag.
func OllamaPullName(alias string) string {
	alias = strings.TrimSpace(alias)
	if i := strings.Index(alias, "/"); i >= 0 {
		return alias[i+1:]
	}
	return alias
}

// HFRepo returns the HuggingFace repo id from a catalog entry.
func HFRepo(entry verify.CatalogEntry) string {
	if entry.HFRepo != nil && strings.TrimSpace(*entry.HFRepo) != "" {
		return strings.TrimSpace(*entry.HFRepo)
	}
	return entry.Alias
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
