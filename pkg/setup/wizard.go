package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/compat"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

// Execute runs the setup wizard.
func Execute(opts Options, io Streams) error {
	if io.Out == nil {
		io.Out = os.Stdout
	}
	if io.Err == nil {
		io.Err = os.Stderr
	}

	path := opts.ConfigPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		path = p
	}

	cfg, err := loadOrFresh(path)
	if err != nil {
		return err
	}

	platformURL := config.ResolvePlatformURL(opts.CatalogURL, cfg.Provider.URL)
	cfg.Provider.URL = platformURL

	fmt.Fprintln(io.Out, "=== Inferoute Client Setup ===")
	fmt.Fprintf(io.Out, "Config: %s\n", path)
	fmt.Fprintf(io.Out, "API:    %s\n\n", platformURL)

	if err := applyAPIKey(cfg, opts, io); err != nil {
		return err
	}
	kind, err := applyEngine(cfg, opts, io)
	if err != nil {
		return err
	}

	detected := engine.Detect(kind)
	autoStart := true
	if !detected.Found {
		doInstall := opts.Install
		if !opts.Yes {
			ok, err := promptYes(io.In, io.Out, fmt.Sprintf("%s is not installed. Install it now?", engineLabel(kind)), true)
			if err != nil {
				return err
			}
			doInstall = ok
		}
		if doInstall {
			fmt.Fprintf(io.Out, "Installing %s...\n", kind)
			if err := engine.Install(context.Background(), kind, io.Out); err != nil {
				fmt.Fprintf(io.Err, "install failed: %v\n", err)
				autoStart = false
			} else {
				detected = engine.Detect(kind)
			}
		} else {
			autoStart = false
			fmt.Fprintf(io.Out, "Skipping install. You can install %s later and re-run inferoute-client setup.\n", kind)
		}
	}
	if detected.Found {
		cfg.Provider.EngineBin = detected.Bin
		fmt.Fprintf(io.Out, "Found %s at %s\n", kind, detected.Bin)
	}

	entry, err := applyModel(cfg, opts, kind, platformURL, io)
	if err != nil {
		return err
	}

	cfg.Provider.Engine = string(kind)
	cfg.Provider.ProviderType = engine.PlatformType(kind)
	cfg.Provider.LLMURL = engine.DefaultURL(kind)
	cfg.Provider.AutoStart = autoStart && detected.Found

	hfRepo := engine.HFRepo(entry)
	spec := engine.ServeSpec(kind, firstNonEmpty(cfg.Provider.EngineBin, detected.Bin), entry.Alias, hfRepo)

	if err := maybeStart(kind, spec, cfg, opts, detected.Found, io); err != nil {
		fmt.Fprintf(io.Err, "warning: %v\n", err)
	}

	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(io.Out, "\nWrote %s\n", path)
	fmt.Fprintln(io.Out, "\nStart the Inferoute client:")
	fmt.Fprintln(io.Out, "  inferoute-client")
	fmt.Fprintf(io.Out, "  inferoute-client --config %s\n", path)
	fmt.Fprintln(io.Out, "\nRe-run this wizard anytime:")
	fmt.Fprintln(io.Out, "  inferoute-client setup")
	return nil
}

func loadOrFresh(path string) (*config.Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := &config.Config{}
		cfg.Server.Port = 8080
		cfg.Server.Host = "127.0.0.1"
		cfg.Server.MaxConcurrentInference = 1
		cfg.Server.SessionQueueWaitSeconds = 90
		cfg.Server.RequestTimeoutSeconds = 240
		cfg.Provider.URL = config.DefaultPlatformURL
		cfg.Provider.LLMTimeoutSeconds = 120
		cfg.Logging.Level = "info"
		cfg.Logging.MaxSize = 100
		cfg.Logging.MaxBackups = 5
		cfg.Logging.MaxAge = 30
		return cfg, nil
	}
	return config.Load(path)
}

func applyAPIKey(cfg *config.Config, opts Options, io Streams) error {
	key := strings.TrimSpace(opts.APIKey)
	if key == "" {
		key = cfg.Provider.APIKey
	}
	if opts.Yes {
		if strings.TrimSpace(key) == "" || key == "your_api_key_here" {
			return fmt.Errorf("--api-key is required with --yes")
		}
		cfg.Provider.APIKey = key
		return nil
	}
	def := ""
	if cfg.HasAPIKey() {
		def = "keep current"
	}
	got, err := promptLine(io.In, io.Out, "Provider API key (from cluster Settings)", def)
	if err != nil && got == "" {
		return fmt.Errorf("API key is required")
	}
	if got == "" || got == "keep current" {
		if !cfg.HasAPIKey() {
			return fmt.Errorf("API key is required")
		}
		return nil
	}
	cfg.Provider.APIKey = got
	return nil
}

func applyEngine(cfg *config.Config, opts Options, io Streams) (engine.Kind, error) {
	if opts.Engine != "" {
		k, ok := engine.ParseKind(opts.Engine)
		if !ok {
			return "", fmt.Errorf("unknown engine %q (ollama, vllm, vllm-metal, freetoken)", opts.Engine)
		}
		return k, nil
	}
	if opts.Yes {
		if cfg.Provider.Engine != "" {
			if k, ok := engine.ParseKind(cfg.Provider.Engine); ok {
				return k, nil
			}
		}
		return "", fmt.Errorf("--engine is required with --yes")
	}

	selectable := make([]engine.Option, 0)
	def := 1
	fmt.Fprintln(io.Out, "Inference engine:")
	for _, o := range engine.HostOptions() {
		if o.Unavailable != "" {
			fmt.Fprintf(io.Out, "     %s — unavailable: %s\n", o.Label, o.Unavailable)
			continue
		}
		selectable = append(selectable, o)
		n := len(selectable)
		if string(o.Kind) == cfg.Provider.Engine {
			def = n
		}
		fmt.Fprintf(io.Out, "  %d) %s\n", n, o.Label)
	}
	if len(selectable) == 0 {
		return "", fmt.Errorf("no engines available on this machine")
	}
	choice, err := promptChoice(io.In, io.Out, len(selectable), def)
	if err != nil {
		return "", err
	}
	return selectable[choice-1].Kind, nil
}

func applyModel(cfg *config.Config, opts Options, kind engine.Kind, platformURL string, io Streams) (verify.CatalogEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var entries []verify.CatalogEntry
	var err error
	if opts.OfflineCatalog != "" {
		entries, err = compat.LoadOfflineCatalog(opts.OfflineCatalog, engine.CatalogType(kind))
	} else {
		msg := fmt.Sprintf("Fetching approved models from %s", platformURL)
		err = spinWhile(io.Out, msg, func() error {
			var fetchErr error
			entries, fetchErr = compat.FetchCatalog(ctx, platformURL, engine.CatalogType(kind))
			return fetchErr
		})
		if err == nil {
			fmt.Fprintf(io.Out, "Loaded %d models from %s\n", len(entries), platformURL)
		}
	}
	if err != nil {
		return verify.CatalogEntry{}, fmt.Errorf("catalog from %s: %w\nSet %s or --catalog-url to a reachable Inferoute API, or pass --offline-catalog", platformURL, err, config.EnvPlatformURL)
	}

	hw, hwErr := compat.Detect()
	if hwErr != nil {
		fmt.Fprintf(io.Err, "hardware detection warning: %v\n", hwErr)
	}
	results := compat.ScoreModels(hw, entries)
	report := compat.BuildReport(hw, results, false)

	if opts.Model != "" {
		for _, e := range entries {
			if e.Alias == opts.Model {
				cfg.Provider.Model = e.Alias
				return e, nil
			}
		}
		return verify.CatalogEntry{}, fmt.Errorf("model %q is not in the %s catalog", opts.Model, engine.CatalogType(kind))
	}
	if opts.Yes {
		if cfg.Provider.Model != "" {
			for _, e := range entries {
				if e.Alias == cfg.Provider.Model {
					return e, nil
				}
			}
		}
		return verify.CatalogEntry{}, fmt.Errorf("--model is required with --yes")
	}

	fmt.Fprintln(io.Out, "\nModels that should fit this machine:")
	if err := compat.WriteTable(io.Out, report); err != nil {
		return verify.CatalogEntry{}, err
	}
	if len(report.Models) == 0 {
		return verify.CatalogEntry{}, fmt.Errorf("no fitting models in the catalog")
	}

	def := 1
	for i, m := range report.Models {
		if m.Alias == cfg.Provider.Model {
			def = i + 1
		}
	}
	choice, err := promptChoice(io.In, io.Out, len(report.Models), def)
	if err != nil {
		return verify.CatalogEntry{}, err
	}
	picked := report.Models[choice-1]
	var entry verify.CatalogEntry
	for _, e := range entries {
		if e.Alias == picked.Alias {
			entry = e
			break
		}
	}
	if entry.Alias == "" {
		entry.Alias = picked.Alias
		entry.HFRepo = picked.HFRepo
	}
	cfg.Provider.Model = entry.Alias
	return entry, nil
}

func maybeStart(kind engine.Kind, spec engine.Spec, cfg *config.Config, opts Options, haveBin bool, io Streams) error {
	if opts.NoStart || !haveBin {
		fmt.Fprintf(io.Out, "\nTo serve this model later:\n  %s\n", spec.CommandLine())
		if kind == engine.KindOllama {
			fmt.Fprintf(io.Out, "  %s\n", engine.PullSpec(spec.Bin, cfg.Provider.Model).CommandLine())
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if engine.Healthy(ctx, kind, cfg.Provider.LLMURL) {
		fmt.Fprintf(io.Out, "\n%s is already running at %s.\n", kind, cfg.Provider.LLMURL)
		fmt.Fprintf(io.Out, "To serve the chosen model, run:\n  %s\n", spec.CommandLine())
		if kind == engine.KindOllama {
			fmt.Fprintf(io.Out, "  %s\n", engine.PullSpec(spec.Bin, cfg.Provider.Model).CommandLine())
		}
		return nil
	}

	startNow := !opts.Yes
	if !opts.Yes {
		ok, err := promptYes(io.In, io.Out, fmt.Sprintf("Start %s with this model now?", kind), true)
		if err != nil {
			return err
		}
		startNow = ok
	} else {
		startNow = true
	}
	if !startNow {
		fmt.Fprintf(io.Out, "\nStart it later with:\n  %s\n", spec.CommandLine())
		return nil
	}

	if kind == engine.KindOllama {
		pull := engine.PullSpec(spec.Bin, cfg.Provider.Model)
		fmt.Fprintf(io.Out, "Pulling %s...\n", engine.OllamaPullName(cfg.Provider.Model))
		pullCtx, pullCancel := context.WithTimeout(context.Background(), engine.DefaultStartTimeout)
		defer pullCancel()
		if err := engine.Run(pullCtx, pull, io.Out); err != nil {
			fmt.Fprintf(io.Err, "warning: ollama pull: %v\n", err)
		}
	}

	fmt.Fprintf(io.Out, "Starting %s...\n", kind)
	logPath := engine.LogPath(cfg.Logging.LogDir)
	if err := engine.StartDetached(spec, logPath); err != nil {
		return err
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), engine.DefaultStartTimeout)
	defer waitCancel()
	fmt.Fprintln(io.Out, "Waiting for the engine to become ready (model load can take several minutes)...")
	if err := engine.WaitHealthy(waitCtx, kind, cfg.Provider.LLMURL, 2*time.Second); err != nil {
		return fmt.Errorf("%w (see %s)", err, logPath)
	}
	fmt.Fprintln(io.Out, "Engine is ready.")
	return nil
}

func engineLabel(k engine.Kind) string {
	switch k {
	case engine.KindVLLMMetal:
		return "vLLM Metal"
	case engine.KindFreeToken:
		return "FreeToken"
	case engine.KindVLLM:
		return "vLLM"
	default:
		return "Ollama"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
