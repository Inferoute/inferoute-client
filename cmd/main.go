package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/compat"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/pricing"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/server"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/tray"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
	"go.uber.org/zap"
)

// Version information (will be set by build flags)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const helpText = `
Inferoute Client - A client for connecting to the Inferoute network

Usage:
  inferoute-client [flags]
  inferoute-client compatibility [flags]

Commands:
  compatibility   Detect local hardware and list which approved models can run
                  (does not start the provider daemon)

Flags:
  --config string   Path to configuration file (default: ~/.config/inferoute/config.yaml)
  --tray            Windows only: hide the console and run in the notification area
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

	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.Usage = func() {
		fmt.Print(helpText)
	}

	configPath := flags.String("config", "", "Path to configuration file")
	trayFlag := flags.Bool("tray", false, "Windows only: hide the console and run in the notification area")
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

	useTray := *trayFlag && tray.Supported()
	if *trayFlag && !tray.Supported() {
		fmt.Fprintln(os.Stderr, "--tray is only supported on Windows; using console")
	}

	fatal := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintln(os.Stderr, msg)
		if useTray {
			showErrorDialog(msg)
		}
		os.Exit(1)
	}

	if *configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fatal("Failed to get user home directory: %v", err)
		}

		configLocations := []string{
			filepath.Join(homeDir, ".config", "inferoute", "config.yaml"),
			"config.yaml",
		}

		for _, location := range configLocations {
			if _, err := os.Stat(location); err == nil {
				*configPath = location
				break
			}
		}

		if *configPath == "" {
			msg := "No configuration file found in standard locations:\n"
			for _, location := range configLocations {
				msg += fmt.Sprintf("  - %s\n", location)
			}
			fatal("%s", msg)
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal("Failed to load configuration: %v", err)
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
		zap.Bool("tray", useTray))

	if useTray {
		hideConsole()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gpuMonitor, err := gpu.NewMonitor()
	if err != nil {
		logger.Error("Failed to initialize GPU monitor", zap.Error(err))
		logger.Warn("Continuing without GPU monitoring")
	}

	llmClient := llm.NewClient(cfg.Provider.ProviderType, cfg.Provider.LLMURL)
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

	serverReady := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			serverReady <- err
		}
	}()

	healthReporter.SetCloudflareClient(srv.GetCloudflareClient())

	go func() {
		ticker := time.NewTicker(health.ReportInterval)
		defer ticker.Stop()

		waitDeadline := time.Now().Add(30 * time.Second)
		for {
			cf := srv.GetCloudflareClient()
			if cf != nil && cf.GetTunnelURL() != "" {
				break
			}
			if time.Now().After(waitDeadline) {
				logger.Warn("Cloudflare tunnel URL not ready before initial health report timeout")
				break
			}
			time.Sleep(1 * time.Second)
		}

		if err := healthReporter.SendHealthReport(ctx); err != nil {
			logger.Error("Failed to send initial health report", zap.Error(err))
		}

		for {
			select {
			case <-ticker.C:
				if err := healthReporter.SendHealthReport(ctx); err != nil {
					logger.Error("Failed to send health report", zap.Error(err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	select {
	case err := <-serverReady:
		fatal("Failed to start server: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	if useTray {
		go func() {
			<-quit
			tray.Quit()
		}()
		tray.Run(tray.Options{
			ConfigPath:   *configPath,
			LogDir:       cfg.Logging.LogDir,
			DashboardURL: cfg.Provider.URL,
		})
	} else {
		<-quit
	}

	logger.Info("Shutting down gracefully...")

	if err := srv.Stop(ctx); err != nil {
		logger.Fatal("Server shutdown failed", zap.Error(err))
	}
}
