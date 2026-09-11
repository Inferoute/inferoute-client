package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

// Spec is the argv used to start an engine.
type Spec struct {
	Bin  string
	Args []string
	URL  string
}

// ServeOpts are catalog-driven flags appended to vLLM / FreeToken serve commands.
type ServeOpts struct {
	ToolCallParser     string
	MaxModelLen        int64
	RopeType           string
	RopeBaseContextLen int64
}

// ServeOptsFromCatalog maps public catalog fields onto ServeOpts.
func ServeOptsFromCatalog(entry verify.CatalogEntry) ServeOpts {
	opts := ServeOpts{}
	if entry.ToolCallParser != nil {
		opts.ToolCallParser = strings.TrimSpace(*entry.ToolCallParser)
	}
	if entry.MaxModelLen != nil {
		opts.MaxModelLen = *entry.MaxModelLen
	}
	if entry.RopeType != nil {
		opts.RopeType = strings.TrimSpace(*entry.RopeType)
	}
	if entry.RopeBaseContextLen != nil {
		opts.RopeBaseContextLen = *entry.RopeBaseContextLen
	}
	return opts
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
func ServeSpec(kind Kind, bin, modelAlias, hfRepo string, opts ServeOpts) Spec {
	if bin == "" {
		bin = defaultBinName(kind)
	}
	spec := Spec{Bin: bin, URL: DefaultURL(kind)}
	switch kind {
	case KindOllama:
		spec.Args = []string{"serve"}
	case KindVLLM, KindVLLMMetal:
		repo := firstNonEmpty(hfRepo, modelAlias)
		spec.Args = []string{"serve", repo, "--host", "127.0.0.1", "--port", "8000"}
		if modelAlias != "" && modelAlias != repo {
			spec.Args = append(spec.Args, "--served-model-name", modelAlias)
		}
		spec.Args = append(spec.Args, vllmServeFlags(opts)...)
	case KindFreeToken:
		repo := firstNonEmpty(hfRepo, modelAlias)
		name := firstNonEmpty(modelAlias, repo)
		spec.Args = []string{"serve", "--model", repo, "--served-model-name", name, "--host", "127.0.0.1", "--port", "1919"}
		if opts.MaxModelLen > 0 {
			spec.Args = append(spec.Args, "--max-seq-len-override", strconv.FormatInt(opts.MaxModelLen, 10))
		}
	}
	return spec
}

func vllmServeFlags(opts ServeOpts) []string {
	var args []string
	if opts.MaxModelLen > 0 {
		args = append(args, "--max-model-len", strconv.FormatInt(opts.MaxModelLen, 10))
	}
	if rope := strings.TrimSpace(opts.RopeType); rope != "" && opts.MaxModelLen > 0 && opts.RopeBaseContextLen > 0 {
		factor := float64(opts.MaxModelLen) / float64(opts.RopeBaseContextLen)
		payload := map[string]any{
			"rope_scaling": map[string]any{
				"rope_type":                         rope,
				"factor":                             factor,
				"original_max_position_embeddings": opts.RopeBaseContextLen,
			},
		}
		raw, err := json.Marshal(payload)
		if err == nil {
			args = append(args, "--hf-overrides", string(raw))
		}
	}
	if parser := strings.TrimSpace(opts.ToolCallParser); parser != "" {
		args = append(args, "--enable-auto-tool-choice", "--tool-call-parser", parser)
	}
	return args
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
