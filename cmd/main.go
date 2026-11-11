package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/benchmark"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/capacity"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/capprobe"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/compat"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/inferscale"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/server"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/setup"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/tray"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/update"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/usermsg"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
	"go.uber.org/zap"
)

//go:generate go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico ../pkg/tray/icon.ico -o rsrc_windows_amd64.syso

const helpText = `
Inferoute Client - A client for connecting to the Inferoute network

Usage:
  inferoute-client [flags]
  inferoute-client setup [flags]
  inferoute-client compatibility [flags]
  inferoute-client update [--check]
  inferoute-client benchmark

Commands:
  setup           Interactive engine, model, and API key wizard (safe to re-run)
  compatibility   Detect local hardware and list which approved models can run
                  (does not start the provider daemon)
  update          Download the newest client, verify it, and replace this binary.
                  Stop the running client first. --check only prints the result.
  benchmark       Ask the running client to measure concurrency again.

Flags:
  --config string   Path to configuration file (default: ~/.config/inferoute/config.yaml)
  --tray            Windows only: run in the notification area (default on Windows)
  --console         Show the terminal dashboard (Windows: do not use the tray)
  --version         Show version information
  --help            Show this help message

For more information, visit: https://github.com/inferoute/inferoute-client
`

func main() {
	enableVirtualTerminal()

	// Subcommands that must not start the provider daemon.
	if len(os.Args) > 1 && os.Args[1] == "compatibility" {
		if err := compat.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "compatibility: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "update" {
		if err := runUpdate(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "update: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		if err := setup.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "benchmark" {
		if err := runBenchmark(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
			os.Exit(1)
		}
		return
	}

	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.Usage = func() {
		fmt.Print(helpText)
	}

	configPath := flags.String("config", "", "Path to configuration file")
	trayFlag := flags.Bool("tray", false, "Windows only: run in the notification area (default on Windows)")
	consoleFlag := flags.Bool("console", false, "Show the terminal dashboard (disables Windows tray mode)")
	showVersion := flags.Bool("version", false, "Show version information")

	if err := flags.Parse(os.Args[1:]); err != nil {
		flags.Usage()
		os.Exit(1)
	}

	if flags.NArg() > 0 && (flags.Arg(0) == "help" || flags.Arg(0) == "--help") {
		flags.Usage()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("inferoute-client %s\n", version.Version)
		fmt.Printf("  commit: %s\n", version.Commit)
		fmt.Printf("  built:  %s\n", version.Date)
		os.Exit(0)
	}

	fatal := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}

	if *configPath == "" {
		resolved, err := resolveConfigPath()
		if err != nil {
			fatal("%s", err.Error())
		}
		*configPath = resolved
	}

	if err := maybeFirstRunSetup(*configPath); err != nil {
		fatal("%s", err.Error())
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal("Failed to load configuration: %v", err)
	}
	config.ApplyEnvOverrides(cfg)
	update.RemoveStaleBackup()
	checkClientVersion(cfg.Provider.URL)
	if !cfg.HasAPIKey() && !cfg.EnrolledWorker() {
		fatal("%s\nOr run: inferoute-client setup", usermsg.InvalidAPIKey)
	}

	useTray := tray.Supported() && !*consoleFlag
	if *trayFlag && !tray.Supported() {
		fmt.Fprintln(os.Stderr, "--tray is only supported on Windows; using console")
	}

	if !isTrayChild() {
		if err := setup.EnsureOnStart(cfg, setup.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: inference engine not ready: %v\n", err)
		}
	}

	if useTray && spawnDetachedIfNeeded(cfg.LocalDashboardURL()) {
		return
	}

	fatal = func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintln(os.Stderr, msg)
		if useTray {
			showErrorDialog(msg)
		}
		os.Exit(1)
	}

	log, err := logger.New(&cfg.Logging)
	if err != nil {
		fatal("Failed to initialize logger: %v", err)
	}
	logger.SetDefaultLogger(log)
	defer log.Logger.Sync()

	logger.Info("Starting Inferoute Provider Client",
		zap.String("config_path", *configPath),
		zap.String("log_level", cfg.Logging.Level),
		zap.String("log_dir", cfg.Logging.LogDir),
		zap.String("engine", cfg.Provider.Engine),
		zap.String("platform_url", cfg.Provider.URL),
		zap.Bool("tray", useTray))

	if useTray {
		hideConsole()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchClientVersion(ctx, cfg.Provider.URL)

	gpuMonitor, err := gpu.NewMonitor()
	if err != nil {
		logger.Error("Failed to initialize GPU monitor", zap.Error(err))
		logger.Warn("Continuing without GPU monitoring")
	}

	llmURL := cfg.Provider.LLMURL
	if cfg.EnrolledWorker() {
		if u := engine.MasterURL(cfg.Provider.EngineArgs); u != "" {
			llmURL = u
		}
	}
	llmClient := llm.NewClient(cfg.Provider.ProviderType, llmURL, cfg.LLMTimeout())

	catalog := verify.NewCatalog(cfg.Provider.URL, cfg.Provider.ProviderType)
	var modelVerifier *verify.Verifier
	if !cfg.EnrolledWorker() {
		if err := catalog.Refresh(ctx); err != nil {
			logger.Warn("Failed to fetch approved model catalog; verification may be limited", zap.Error(err))
		} else {
			logger.Info("Loaded approved model catalog",
				zap.Strings("aliases", catalog.Aliases()))
		}
		serverClient := verify.NewServerClient(cfg.Provider.URL, cfg.Provider.APIKey)
		modelVerifier = verify.NewVerifier(catalog, serverClient, cfg.Provider.ProviderType, cfg.Provider.HFHubCache, cfg.Provider.ModelPath)
	}

	healthReporter := health.NewReporter(cfg, gpuMonitor, llmClient)
	healthReporter.SetVerifier(modelVerifier)

	srv := server.CreateServer(cfg, gpuMonitor, healthReporter, modelVerifier)
	if useTray {
		srv.SetConsoleUI(false)
	}
	startCapacity(ctx, cfg, *configPath, llmURL, llmClient, gpuMonitor, catalog, srv, healthReporter)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	go func() {
		interval := health.ReportInterval
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		upgradeLogged := false

		report := func(msg string) bool {
			err := healthReporter.SendHealthReport(ctx)
			if setup.NodeRemoved(err) {
				logger.Error("The owner removed this machine from the cluster. Re-run setup and paste a new join code.", zap.Error(err))
				return false
			}
			next := nodeapi.HealthInterval(err, health.ReportInterval)
			if next != interval {
				interval = next
				ticker.Reset(interval)
			}
			if err == nil {
				upgradeLogged = false
				return true
			}
			if nodeapi.IsUpgradeRequired(err) {
				if !upgradeLogged {
					logger.Error(msg, zap.Error(err))
					upgradeLogged = true
				}
				return true
			}
			logger.Error(msg, zap.Error(err))
			return true
		}

		if ctx.Err() != nil {
			return
		}
		if !report("Failed to send initial health report") {
			return
		}

		for {
			select {
			case <-ticker.C:
				if !report("Failed to send health report") {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start() blocks in ListenAndServe after ~10s of tunnel startup. A 100ms
	// wait returned long before tunnel/listen failures, so the process sat in
	// the tray with a dead server while health still advertised the tunnel.
	var (
		exitMu  sync.Mutex
		exitErr error
	)
	setExitErr := func(err error) {
		if !isFatalServerErr(err) {
			return
		}
		exitMu.Lock()
		if exitErr == nil {
			exitErr = err
		}
		exitMu.Unlock()
	}

	if useTray {
		// RequestTunnel can fail in tens of ms, before tray.Run has an event
		// loop. systray.Quit is a no-op until then, so drain first and fatal
		// without entering the tray. Slow failures wait for Started.
		select {
		case err := <-serverErr:
			if isFatalServerErr(err) {
				fatal("%s", usermsg.Startup(err))
			}
		default:
		}
		trayStarted := make(chan struct{})
		go func() {
			select {
			case <-quit:
			case err := <-serverErr:
				if isFatalServerErr(err) {
					setExitErr(err)
					logger.Error("Server failed", zap.Error(err))
					showErrorDialog(usermsg.Startup(err))
				}
			}
			<-trayStarted
			tray.Quit()
		}()
		tray.Run(tray.Options{
			ConfigPath:   *configPath,
			LogDir:       cfg.Logging.LogDir,
			DashboardURL: cfg.LocalDashboardURL(),
			Started:      trayStarted,
		})
	} else {
		select {
		case <-quit:
		case err := <-serverErr:
			if isFatalServerErr(err) {
				setExitErr(err)
				logger.Error("Server failed", zap.Error(err))
				fmt.Fprintln(os.Stderr, usermsg.Startup(err))
			}
		}
	}

	logger.Info("Shutting down gracefully...")
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := srv.Stop(stopCtx); err != nil {
		logger.Fatal("Server shutdown failed", zap.Error(err))
	}

	exitMu.Lock()
	err = exitErr
	exitMu.Unlock()
	if err != nil {
		os.Exit(1)
	}
}

func isFatalServerErr(err error) bool {
	return err != nil && !errors.Is(err, http.ErrServerClosed)
}

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "print whether an update is available")
	configPath := fs.String("config", "", "path to configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	platform := ""
	path := *configPath
	if path == "" {
		path, _ = resolveConfigPath()
	}
	if path != "" {
		if cfg, err := config.Load(path); err == nil {
			config.ApplyEnvOverrides(cfg)
			platform = cfg.Provider.URL
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return update.Apply(ctx, platform, *checkOnly)
}

func watchClientVersion(ctx context.Context, platformURL string) {
	if os.Getenv("INFEROUTE_IN_DOCKER") == "1" || platformURL == "" {
		return
	}
	const timeout = 30 * time.Second
	var lastNotice string
	var warned bool
	check := func() {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		rel, err := update.FetchRelease(reqCtx, platformURL)
		if err != nil {
			if !warned {
				logger.Warn("version check skipped", zap.Error(err))
				warned = true
			}
			return
		}
		warned = false
		update.Remember(rel)
		msg, _ := update.Notice(rel)
		if msg == lastNotice {
			return
		}
		lastNotice = msg
		if msg != "" {
			logger.Info(msg)
		}
	}
	check()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func checkClientVersion(platformURL string) {
	if os.Getenv("INFEROUTE_IN_DOCKER") == "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rel, err := update.FetchRelease(ctx, platformURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "version check skipped: %v\n", err)
		return
	}
	update.Remember(rel)
	msg, required := update.Notice(rel)
	if msg == "" {
		return
	}
	if required {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
	fmt.Println(msg)
}

func resolveConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	locations := []string{
		filepath.Join(homeDir, ".config", "inferoute", "config.yaml"),
		"config.yaml",
	}
	for _, location := range locations {
		if _, err := os.Stat(location); err == nil {
			return location, nil
		}
	}
	return locations[0], nil
}

func maybeFirstRunSetup(configPath string) error {
	needs := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		needs = true
	} else if err != nil {
		return err
	} else {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		needs = !cfg.HasAPIKey() && !cfg.EnrolledWorker()
	}
	if !needs {
		return nil
	}
	if !setup.IsInteractive() {
		return fmt.Errorf("no valid configuration at %s\nRun: inferoute-client setup", configPath)
	}
	fmt.Println("No provider API key configured. Starting setup...")
	return setup.Run([]string{"--config", configPath})
}

func runBenchmark(args []string) error {
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := *configPath
	if path == "" {
		var err error
		path, err = resolveConfigPath()
		if err != nil {
			return err
		}
	}
	if err := benchmark.Clear(path); err != nil {
		return err
	}
	fmt.Println("benchmark requested; the running client measures on its next check")
	return nil
}

func startCapacity(ctx context.Context, cfg *config.Config, configPath, llmURL string, llmClient llm.Client, gpuMonitor *gpu.Monitor, catalog *verify.Catalog, srv *server.Server, reporter *health.Reporter) {
	if cfg.EnrolledWorker() || cfg.Provider.ClusterMode == "worker" {
		return
	}
	sender := capacity.NewSender(cfg.Provider.URL, cfg.Provider.APIKey, srv.SlotSnapshot, llmClient)
	srv.SetCapacityNotify(sender.Notify)
	go sender.Loop(ctx)

	// vLLM without a configured limit measures its concurrency. Close the gate
	// before the probes start, so their first round cannot delay that.
	_, explicit := cfg.ExplicitConcurrency()
	benchmarks := !explicit && cfg.Provider.Engine != "llama_cpp"
	if benchmarks {
		srv.SetBenchmarking(true)
	}
	go (&capprobe.Watcher{
		EngineURL:   llmURL,
		Model:       cfg.Provider.Model,
		PlatformURL: cfg.Provider.URL,
		APIKey:      cfg.Provider.APIKey,
		Fingerprint: func() string { return deploymentFingerprint(ctx, cfg, llmClient, gpuMonitor) },
		// Not accepting means the concurrency benchmark owns the engine.
		Idle: func() bool {
			inflight, _, accepting := srv.SlotSnapshot()
			return inflight == 0 && accepting
		},
		// Each probe holds a slot, so the benchmark waits for it.
		Claim: srv.ClaimIdleSlot,
		Note:  srv.SetCapabilities,
	}).Loop(ctx)

	if !benchmarks {
		return
	}
	kind, _ := engine.ParseKind(cfg.Provider.Engine)
	go (&benchmark.Watcher{
		ConfigPath: configPath,
		EngineURL:  llmURL,
		Model:      cfg.Provider.Model,
		Ready: func() bool {
			probe, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if cfg.Provider.ClusterMode == "master" {
				_, online := inferscale.Tailnet()
				return engine.MasterState(probe, kind, llmURL, cfg.Provider.EngineArgs, online) == "ready"
			}
			return engine.Healthy(probe, kind, llmURL)
		},
		Fingerprint: func() string { return deploymentFingerprint(ctx, cfg, llmClient, gpuMonitor) },
		Recommended: func() int {
			entry, ok := catalog.Get(cfg.Provider.Model)
			if ok && entry.RecommendedConcurrency != nil && *entry.RecommendedConcurrency > 0 {
				return int(*entry.RecommendedConcurrency)
			}
			return engine.DefaultConcurrency(kind)
		},
		Apply: func(n int) { srv.SetMaxInflight(int32(n)) },
		Note:  srv.SetEngineNote,
		Gate:  srv.SetBenchmarking,
		Inflight: func() int32 {
			n, _, _ := srv.SlotSnapshot()
			return n
		},
		GPUBusy: func() bool {
			if gpuMonitor == nil {
				return false
			}
			info, err := gpuMonitor.GetGPUInfo()
			return err == nil && info != nil && info.Utilization > 20
		},
		Preemptions: func(c context.Context) int64 {
			src, ok := llmClient.(interface {
				EngineCapacity(context.Context) (llm.EngineCapacity, error)
			})
			if !ok {
				return 0
			}
			got, err := src.EngineCapacity(c)
			if err != nil || got.Preemptions == nil {
				return 0
			}
			return *got.Preemptions
		},
		Record: func(res benchmark.Result) {
			reporter.SetBenchmark(&health.BenchmarkSummary{
				Concurrency: res.Concurrency,
				MeasuredAt:  res.MeasuredAt,
				Levels:      res.Levels,
			})
		},
	}).Loop(ctx)
}

// deploymentFingerprint identifies what is being served: model, engine and its
// version, weights, GPUs, and the engine's launch arguments. It is empty while
// the engine does not answer, so a half-started engine is not mistaken for a
// different deployment.
func deploymentFingerprint(ctx context.Context, cfg *config.Config, llmClient llm.Client, gpuMonitor *gpu.Monitor) string {
	fp := cfg.Provider.Model + "|" + cfg.Provider.Engine + "|" + engine.InstalledVersion(cfg.Provider.EngineBin)
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	listed, err := llmClient.ListModels(probe)
	if err != nil {
		return ""
	}
	for _, m := range listed.Models {
		if m.ID == cfg.Provider.Model || cfg.Provider.Model == "" {
			fp += "|" + m.WeightFingerprint + "|" + fmt.Sprint(m.MaxModelLen)
			break
		}
	}
	if gpuMonitor != nil {
		if info, err := gpuMonitor.GetGPUInfo(); err == nil && info != nil {
			fp += "|" + info.ProductName + "|" + fmt.Sprint(info.GPUCount)
		}
	}
	fp += "|" + fmt.Sprint(engine.NNodes(cfg.Provider.EngineArgs))
	fp += "|" + engineArgsDigest(cfg.Provider.EngineArgs)
	return fp
}

// engineArgsDigest changes when the engine's launch arguments change (a tool
// parser turned on, say). It is a hash because the fingerprint is sent to the
// platform and the arguments can hold an API key.
func engineArgsDigest(args []string) string {
	sum := sha256.Sum256([]byte(strings.Join(args, "\x00")))
	return hex.EncodeToString(sum[:6])
}
