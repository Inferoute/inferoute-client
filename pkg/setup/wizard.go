package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/compat"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/inferscale"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

// hostGOOS is runtime.GOOS; tests override it to cover Windows/macOS remap.
var hostGOOS = runtime.GOOS

// detectEngine and installEngine are replaced in tests.
var (
	detectEngine  = engine.Detect
	installEngine = engine.Install
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

	inferscale.WithWait = func(msg string, fn func() error) error {
		return SpinWhile(io.Out, msg, fn)
	}
	fmt.Fprintln(io.Out, "=== Inferoute Client Setup ===")
	fmt.Fprintf(io.Out, "Config: %s\n", path)
	fmt.Fprintf(io.Out, "API:    %s\n\n", platformURL)

	if err := applyAPIKey(cfg, opts, io); err != nil {
		return err
	}
	var plan clusterSettings
	var planErr error
	if err := SpinWhile(io.Out, "Reading cluster settings", func() error {
		plan, planErr = clusterPlan(cfg)
		return nil
	}); err != nil {
		return err
	}
	if planErr != nil && plan.Role != "single" {
		return planErr
	}

	var kind engine.Kind
	if plan.Role == "master" || plan.Role == "worker" {
		kind, err = resolveEngine(hostGOOS, firstNonEmpty(plan.ServiceType, "vllm"))
		if err != nil {
			return err
		}
		if plan.Role == "master" {
			printCluster(io.Out, plan)
		}
	} else {
		kind, err = applyEngine(cfg, opts, io)
		if err != nil {
			return err
		}
	}

	detected := detectEngine(kind)
	autoStart := true
	if !detected.Found {
		doInstall := opts.Install || (plan.Role == "worker")
		if !doInstall && !opts.Yes {
			q := fmt.Sprintf("%s is not installed. Install it now?", engine.Label(kind))
			if detected.Unusable != "" {
				q = fmt.Sprintf("%s at %s cannot run (%s). Reinstall it now?", engine.Label(kind), detected.Bin, detected.Unusable)
			}
			ok, err := promptYes(io.In, io.Out, q, true)
			if err != nil {
				return err
			}
			doInstall = ok
		}
		if doInstall {
			fmt.Fprintf(io.Out, "Installing %s...\n", engine.Label(kind))
			if err := installEngine(context.Background(), kind, io.Out); err != nil {
				fmt.Fprintf(io.Err, "install failed: %v\n", err)
				autoStart = false
			} else {
				detected = detectEngine(kind)
			}
		} else {
			autoStart = false
			fmt.Fprintf(io.Out, "Skipping install. You can install %s later and re-run inferoute-client setup.\n", kind)
		}
	}
	if detected.Found {
		cfg.Provider.EngineBin = detected.Bin
		fmt.Fprintf(io.Out, "Found %s at %s\n", kind, detected.Bin)
	} else {
		cfg.Provider.EngineBin = ""
	}

	var entry verify.CatalogEntry
	if plan.Role == "master" || plan.Role == "worker" {
		entry = clusterEntry(opts, kind, platformURL, plan, io)
		cfg.Provider.Model = entry.Alias
	} else {
		entry, err = applyModel(cfg, opts, kind, platformURL, io)
		if err != nil {
			return err
		}
	}

	cfg.Provider.Engine = string(kind)
	cfg.Provider.ProviderType = engine.PlatformType(kind)
	cfg.Provider.LLMURL = engine.DefaultURL(kind)
	cfg.Provider.AutoStart = autoStart && detected.Found

	hfRepo := engine.HFRepo(entry)
	applyServeOpts(cfg, entry, hfRepo)
	serveOpts := engine.ServeOptsFromCatalog(entry)
	spec := engine.ServeSpec(kind, firstNonEmpty(cfg.Provider.EngineBin, detected.Bin), entry.Alias, hfRepo, serveOpts)
	if err := prepareCluster(cfg, &spec); err != nil {
		if saveErr := config.Save(path, cfg); saveErr != nil {
			return saveErr
		}
		return err
	}

	if plan.Role == "worker" {
		printCluster(io.Out, plan)
	}
	if err := maybeStart(kind, spec, cfg, opts, detected.Found, io); err != nil {
		if saveErr := config.Save(path, cfg); saveErr != nil {
			return saveErr
		}
		fmt.Fprintf(io.Out, "\nWrote %s\n", path)
		fmt.Fprintf(io.Err, "Setup did not finish: %s is not serving yet (see %s).\n", engine.Label(kind), engine.LogPath(cfg.Logging.LogDir))
		fmt.Fprintf(io.Err, "Leave the engine running, then re-run: inferoute-client setup\n")
		return err
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
	if opts.Yes || strings.TrimSpace(opts.APIKey) != "" {
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
		return resolveEngine(hostGOOS, opts.Engine)
	}
	if opts.Yes {
		if cfg.Provider.Engine != "" {
			if k, err := resolveEngine(hostGOOS, cfg.Provider.Engine); err == nil {
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

func resolveEngine(goos, name string) (engine.Kind, error) {
	k, ok := engine.ParseKind(name)
	if !ok {
		return "", fmt.Errorf("unknown engine %q (vllm, vllm-metal, llama_cpp)", name)
	}
	return engine.RuntimeKind(goos, k), nil
}

func printCluster(out io.Writer, plan clusterSettings) {
	name := firstNonEmpty(plan.Name, "this cluster")
	if plan.Role == "worker" {
		fmt.Fprintf(out, "\nJoining cluster %s\n", name)
	} else {
		fmt.Fprintf(out, "\nCluster %s\n", name)
	}
	fmt.Fprintf(out, "Model: %s\n", modelLabel(plan))
}

func modelLabel(plan clusterSettings) string {
	name := strings.TrimSpace(plan.DisplayName)
	alias := strings.TrimSpace(plan.Model)
	if name == "" || strings.EqualFold(name, alias) {
		return alias
	}
	return name + " (" + alias + ")"
}

func clusterEntry(opts Options, kind engine.Kind, platformURL string, plan clusterSettings, io Streams) verify.CatalogEntry {
	fallback := verify.CatalogEntry{Alias: plan.Model, ServiceType: "vllm"}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var entries []verify.CatalogEntry
	var err error
	if opts.OfflineCatalog != "" {
		entries, err = compat.LoadOfflineCatalog(opts.OfflineCatalog, engine.CatalogType(kind))
	} else {
		err = SpinWhile(io.Out, "Loading the cluster model", func() error {
			var fetchErr error
			entries, fetchErr = compat.FetchCatalog(ctx, platformURL, engine.CatalogType(kind))
			return fetchErr
		})
	}
	if err != nil {
		fmt.Fprintf(io.Err, "catalog unavailable (%v); serving %s without catalog flags\n", err, plan.Model)
		return fallback
	}
	entries = verify.FilterByEngine(entries, string(kind))
	for _, e := range entries {
		if e.Alias == plan.Model {
			return e
		}
	}
	return fallback
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
		err = SpinWhile(io.Out, msg, func() error {
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

	before := len(entries)
	entries = verify.FilterByEngine(entries, string(kind))
	if before > 0 && len(entries) < before {
		fmt.Fprintf(io.Out, "Filtered to %d models supported by %s\n", len(entries), engine.Label(kind))
	}

	hw, hwErr := compat.Detect()
	if hwErr != nil {
		fmt.Fprintf(io.Err, "hardware detection warning: %v\n", hwErr)
	}
	results := compat.ScoreModels(hw, entries, kind)
	report := compat.BuildReport(hw, results, false)

	if opts.Model != "" {
		for _, e := range entries {
			if e.Alias == opts.Model {
				cfg.Provider.Model = e.Alias
				return e, nil
			}
		}
		return verify.CatalogEntry{}, fmt.Errorf("model %q is not supported by %s", opts.Model, engine.Label(kind))
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

func applyServeOpts(cfg *config.Config, entry verify.CatalogEntry, hfRepo string) {
	opts := engine.ServeOptsFromCatalog(entry)
	cfg.Provider.ToolCallParser = opts.ToolCallParser
	cfg.Provider.MaxModelLen = opts.MaxModelLen
	cfg.Provider.RopeType = opts.RopeType
	cfg.Provider.RopeBaseContextLen = opts.RopeBaseContextLen
	if hfRepo != "" && hfRepo != entry.Alias {
		cfg.Provider.HFRepo = hfRepo
	} else {
		cfg.Provider.HFRepo = ""
	}
	if entry.GGUFFile != nil {
		cfg.Provider.GGUFFile = strings.TrimSpace(*entry.GGUFFile)
	} else {
		cfg.Provider.GGUFFile = ""
	}
}

func formatSpec(spec engine.Spec) string {
	if len(spec.Env) == 0 {
		return spec.CommandLine()
	}
	return strings.Join(spec.Env, " ") + " " + spec.CommandLine()
}

func maybeStart(kind engine.Kind, spec engine.Spec, cfg *config.Config, opts Options, haveBin bool, io Streams) error {
	if opts.NoStart || !haveBin {
		fmt.Fprintf(io.Out, "\nTo serve this model later:\n  %s\n", formatSpec(spec))
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if engine.Healthy(ctx, kind, cfg.Provider.LLMURL) {
		fmt.Fprintf(io.Out, "\n%s is already running at %s.\n", kind, cfg.Provider.LLMURL)
		fmt.Fprintf(io.Out, "To serve the chosen model, run:\n  %s\n", formatSpec(spec))
		return nil
	}
	model := strings.TrimSpace(cfg.Provider.Model)
	if model == "" {
		model = "the model"
	}
	waitMsg := fmt.Sprintf("Waiting for %s to finish downloading and start serving at %s", model, cfg.Provider.LLMURL)

	if engine.PortOpen(ctx, cfg.Provider.LLMURL) {
		fmt.Fprintf(io.Out, "\n%s is already starting at %s (port in use).\n", kind, cfg.Provider.LLMURL)
		waitCtx, waitCancel := context.WithTimeout(context.Background(), engine.DefaultDownloadTimeout)
		defer waitCancel()
		err := SpinWhile(io.Out, waitMsg, func() error {
			return engine.WaitHealthy(waitCtx, kind, cfg.Provider.LLMURL, 2*time.Second, nil)
		})
		if err != nil {
			return fmt.Errorf("%w (see %s)", err, engine.LogPath(cfg.Logging.LogDir))
		}
		fmt.Fprintf(io.Out, "Engine is ready at %s.\n", cfg.Provider.LLMURL)
		return nil
	}

	fmt.Fprintf(io.Out, "\n%s\n", formatSpec(spec))
	startNow := opts.Yes
	if !opts.Yes {
		ok, err := promptYes(io.In, io.Out, fmt.Sprintf("Start %s with this model now?", kind), true)
		if err != nil {
			return err
		}
		startNow = ok
	}
	if !startNow {
		return nil
	}

	fmt.Fprintf(io.Out, "Starting %s...\n", kind)
	logPath := engine.LogPath(cfg.Logging.LogDir)
	exited, err := engine.StartDetached(spec, logPath)
	if err != nil {
		return err
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), engine.DefaultDownloadTimeout)
	defer waitCancel()
	if err := SpinWhile(io.Out, waitMsg, func() error {
		return engine.WaitHealthy(waitCtx, kind, cfg.Provider.LLMURL, 2*time.Second, exited)
	}); err != nil {
		return fmt.Errorf("%w (see %s)", err, logPath)
	}
	fmt.Fprintf(io.Out, "Engine is ready at %s.\n", cfg.Provider.LLMURL)
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
