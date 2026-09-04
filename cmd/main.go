package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/compat"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/pricing"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/server"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/setup"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/tray"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/usermsg"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
	"go.uber.org/zap"
)

// Version information (will be set by build flags)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

//go:generate go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico ../pkg/tray/icon.ico -o rsrc_windows_amd64.syso

const helpText = `
Inferoute Client - A client for connecting to the Inferoute network

Usage:
  inferoute-client [flags]
  inferoute-client setup [flags]
  inferoute-client compatibility [flags]

Commands:
  setup           Interactive engine, model, and API key wizard (safe to re-run)
  compatibility   Detect local hardware and list which approved models can run
                  (does not start the provider daemon)

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
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		if err := setup.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
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
		fmt.Printf("inferoute-client %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
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

	useTray := tray.Supported() && !*consoleFlag
	if *trayFlag && !tray.Supported() {
		fmt.Fprintln(os.Stderr, "--tray is only supported on Windows; using console")
	}
	if useTray && spawnDetachedIfNeeded() {
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

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal("Failed to load configuration: %v", err)
	}
	if !cfg.HasAPIKey() {
		fatal("%s\nOr run: inferoute-client setup", usermsg.InvalidAPIKey)
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
		zap.Bool("tray", useTray))

	if useTray {
		hideConsole()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.Provider.AutoStart {
		startCtx, startCancel := context.WithTimeout(ctx, engine.DefaultStartTimeout)
		if err := engine.EnsureReady(startCtx, cfg, cfg.Logging.LogDir); err != nil {
			logger.Warn("Local inference engine is not ready", zap.Error(err))
			fmt.Fprintf(os.Stderr, "warning: inference engine not ready: %v\n", err)
		}
		startCancel()
	}

	gpuMonitor, err := gpu.NewMonitor()
	if err != nil {
		logger.Error("Failed to initialize GPU monitor", zap.Error(err))
		logger.Warn("Continuing without GPU monitoring")
	}

	llmClient := llm.NewClient(cfg.Provider.ProviderType, cfg.Provider.LLMURL, cfg.LLMTimeout())
	pricingClient := pricing.NewClient(cfg.Provider.URL, cfg.Provider.APIKey)

	catalog := verify.NewCatalog(cfg.Provider.URL, cfg.Provider.ProviderType)
	if err := catalog.Refresh(ctx); err != nil {
		logger.Warn("Failed to fetch approved model catalog; verification may be limited", zap.Error(err))
	} else {
		logger.Info("Loaded approved model catalog",
			zap.Strings("aliases", catalog.Aliases()))
	}
	serverClient := verify.NewServerClient(cfg.Provider.URL, cfg.Provider.APIKey)
	modelVerifier := verify.NewVerifier(catalog, serverClient, cfg.Provider.ProviderType, cfg.Provider.HFHubCache, cfg.Provider.ModelPath)

	var registeredModelIDs []string
	if ids, err := pricing.RegisterLocalModels(ctx, llmClient, pricingClient, cfg.Provider.ProviderType, modelVerifier); err != nil {
		logger.Error("Failed to register local models", zap.Error(err))
	} else {
		registeredModelIDs = ids
	}

	healthReporter := health.NewReporter(cfg, gpuMonitor, llmClient)
	healthReporter.SetVerifier(modelVerifier)
	healthReporter.InitializeRegisteredModels(registeredModelIDs)

	srv := server.CreateServer(cfg, gpuMonitor, healthReporter, modelVerifier)
	if useTray {
		srv.SetConsoleUI(false)
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	healthReporter.SetCloudflareClient(srv.GetCloudflareClient())

	go func() {
		ticker := time.NewTicker(health.ReportInterval)
		defer ticker.Stop()

		// StartTunnel holds ~10s after RequestTunnel sets the hostname, so a
		// URL is not proof the tunnel is up. Wait for the supervised process.
		waitDeadline := time.Now().Add(45 * time.Second)
		for {
			if ctx.Err() != nil {
				return
			}
			cf := srv.GetCloudflareClient()
			if cf != nil && cf.IsRunning() && cf.GetTunnelURL() != "" {
				break
			}
			if time.Now().After(waitDeadline) {
				logger.Warn("Cloudflare tunnel not running before initial health report timeout")
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}

		if ctx.Err() != nil {
			return
		}
		cf := srv.GetCloudflareClient()
		if cf == nil || !cf.IsRunning() {
			logger.Warn("Skipping initial health report; tunnel is not running")
		} else if err := healthReporter.SendHealthReport(ctx); err != nil {
			logger.Error("Failed to send initial health report", zap.Error(err))
		}

		for {
			select {
			case <-ticker.C:
				if err := healthReporter.SendHealthReport(ctx); err != nil {
					logger.Error("Failed to send health report", zap.Error(err))
				}
			case <-healthReporter.BusyChanges():
				// Busy-state transition: push immediately so the central
				// provider_busy flag is seconds, not minutes, stale.
				if err := healthReporter.SendHealthReport(ctx); err != nil {
					logger.Error("Failed to send busy-transition health report", zap.Error(err))
				}
				// Cool down so rapid request bursts don't flood the platform.
				// A transition during the cooldown leaves a pending signal
				// (buffered channel), which sends a fresh report right after.
				select {
				case <-time.After(10 * time.Second):
				case <-ctx.Done():
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
		needs = !cfg.HasAPIKey()
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
