package setup

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const helpText = `Usage:
  inferoute-client setup [flags]

Walk through engine, model, and API key setup. Writes ~/.config/inferoute/config.yaml.
Safe to re-run; existing values are used as defaults.

Flags:
  --config string        Config path (default: ~/.config/inferoute/config.yaml)
  --engine string        ollama, vllm, vllm-metal, or freetoken
  --model string         Catalog alias to serve
  --api-key string       Provider API key (or PROVIDER_API_KEY)
  --catalog-url string   Inferoute API base (default: https://core.inferoute.com)
  --offline-catalog path Load catalog JSON from a file
  --yes                  Non-interactive: require --engine, --model, --api-key
  --install              Install the engine if it is missing (--yes does not install)
  --no-start             Do not start the engine after writing config
  --help                 Show this help
`

// Options configures the setup wizard.
type Options struct {
	ConfigPath     string
	Engine         string
	Model          string
	APIKey         string
	CatalogURL     string
	OfflineCatalog string
	Yes            bool
	Install        bool
	NoStart        bool
}

// Run parses args and runs the wizard.
func Run(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := Options{CatalogURL: "https://core.inferoute.com"}
	fs.StringVar(&opts.ConfigPath, "config", "", "Path to configuration file")
	fs.StringVar(&opts.Engine, "engine", "", "Engine: ollama, vllm, vllm-metal, freetoken")
	fs.StringVar(&opts.Model, "model", "", "Catalog alias")
	fs.StringVar(&opts.APIKey, "api-key", "", "Provider API key")
	fs.StringVar(&opts.CatalogURL, "catalog-url", "https://core.inferoute.com", "Inferoute catalog base URL")
	fs.StringVar(&opts.OfflineCatalog, "offline-catalog", "", "Path to offline approved-builds JSON")
	fs.BoolVar(&opts.Yes, "yes", false, "Non-interactive")
	fs.BoolVar(&opts.Install, "install", false, "Install the engine if missing")
	fs.BoolVar(&opts.NoStart, "no-start", false, "Do not start the engine")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, helpText)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		if arg == "help" || arg == "--help" {
			fs.Usage()
			return nil
		}
		return fmt.Errorf("unexpected argument: %s", arg)
	}
	if env := strings.TrimSpace(os.Getenv("PROVIDER_API_KEY")); opts.APIKey == "" && env != "" {
		opts.APIKey = env
	}
	if env := strings.TrimSpace(os.Getenv("PROVIDER_TYPE")); opts.Engine == "" && env != "" {
		opts.Engine = env
	}

	in, closer, err := inputReader()
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	return Execute(opts, Streams{In: in, Out: os.Stdout, Err: os.Stderr})
}

// Streams is the wizard I/O.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}
