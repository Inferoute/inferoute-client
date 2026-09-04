package compat

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

const compatibilityHelp = `Usage:
  inferoute-client compatibility [flags]

Detect local hardware and list which approved Inferoute models can run on this machine.
Does not start the provider daemon. Does not require an API key.

Flags:
  --provider-type string   Filter catalog: ollama, vllm, or empty for both (default: both)
  --catalog-url string     Inferoute API base URL (default: https://core.inferoute.com, or INFEROUTE_URL)
  --offline-catalog path   Load catalog JSON from a local file instead of the network
  --json                   Emit machine-readable JSON
  --show-too-large         Include too_large models in table/JSON model list (default: true)
  --help                   Show this help
`

// Options configures the compatibility command.
type Options struct {
	ProviderType   string
	CatalogURL     string
	OfflineCatalog string
	JSON           bool
	ShowTooLarge   bool
}

// Run parses args and prints the compatibility report.
func Run(args []string) error {
	fs := flag.NewFlagSet("compatibility", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := Options{
		ShowTooLarge: true,
	}
	fs.StringVar(&opts.ProviderType, "provider-type", "", "Filter by provider type: ollama, vllm, or empty for both")
	fs.StringVar(&opts.CatalogURL, "catalog-url", "", "Inferoute catalog base URL")
	fs.StringVar(&opts.OfflineCatalog, "offline-catalog", "", "Path to offline approved-builds JSON")
	fs.BoolVar(&opts.JSON, "json", false, "Emit JSON output")
	showTooLarge := fs.Bool("show-too-large", true, "Include too_large models in the model list")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, compatibilityHelp)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	opts.ShowTooLarge = *showTooLarge

	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		if arg == "help" || arg == "--help" {
			fs.Usage()
			return nil
		}
		return fmt.Errorf("unexpected argument: %s", arg)
	}

	pt := strings.ToLower(strings.TrimSpace(opts.ProviderType))
	if pt != "" && pt != "ollama" && pt != "vllm" {
		return fmt.Errorf("--provider-type must be ollama, vllm, or empty")
	}
	opts.ProviderType = pt
	if opts.CatalogURL == "" {
		opts.CatalogURL = config.ResolvePlatformURL("", "")
	}

	return Execute(opts)
}

// Execute runs detection + scoring with the given options.
func Execute(opts Options) error {
	hw, err := Detect()
	if err != nil {
		return fmt.Errorf("hardware detection: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	catalogEntries, err := loadEntries(ctx, opts)
	if err != nil {
		return err
	}

	results := ScoreModels(hw, catalogEntries)
	report := BuildReport(hw, results, opts.ShowTooLarge)

	if opts.JSON {
		return WriteJSON(os.Stdout, report)
	}
	return WriteTable(os.Stdout, report)
}

func loadEntries(ctx context.Context, opts Options) ([]verify.CatalogEntry, error) {
	if opts.OfflineCatalog != "" {
		return LoadOfflineCatalog(opts.OfflineCatalog, opts.ProviderType)
	}
	return FetchCatalog(ctx, opts.CatalogURL, opts.ProviderType)
}
