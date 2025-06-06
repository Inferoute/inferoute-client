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
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/pricing"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/server"
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

Flags:
  --config string   Path to configuration file (default: ~/.config/inferoute/config.yaml)
  --version        Show version information
  --help          Show this help message

For more information, visit: https://github.com/inferoute/inferoute-client
`

func main() {
	// Create custom flag set
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.Usage = func() {
		fmt.Print(helpText)
	}

	// Parse command line flags
	configPath := flags.String("config", "", "Path to configuration file")
	showVersion := flags.Bool("version", false, "Show version information")

	// Parse flags
	if err := flags.Parse(os.Args[1:]); err != nil {
		flags.Usage()
		os.Exit(1)
	}

	// Show help if requested
	if flags.NArg() > 0 && (flags.Arg(0) == "help" || flags.Arg(0) == "--help") {
		flags.Usage()
		os.Exit(0)
	}

	// Show version and exit if requested
	if *showVersion {
		fmt.Printf("inferoute-client %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
		os.Exit(0)
	}

	// If no config path is provided, check standard locations
	if *configPath == "" {
		// Get user's home directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Printf("Failed to get user home directory: %v\n", err)
			os.Exit(1)
		}

		// Check standard config locations
		configLocations := []string{
			filepath.Join(homeDir, ".config", "inferoute", "config.yaml"),
			"config.yaml", // Current directory
		}

		for _, location := range configLocations {
			if _, err := os.Stat(location); err == nil {
				*configPath = location
				break
			}
		}

		if *configPath == "" {
			fmt.Printf("No configuration file found in standard locations:\n")
			for _, location := range configLocations {
				fmt.Printf("  - %s\n", location)
			}
			os.Exit(1)
		}
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.New(&cfg.Logging)
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	logger.SetDefaultLogger(log)
	defer log.Logger.Sync()

	logger.Info("Starting Inferoute Provider Client",
		zap.String("config_path", *configPath),
		zap.String("log_level", cfg.Logging.Level),
		zap.String("log_dir", cfg.Logging.LogDir))

	// Create context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize GPU monitor
	gpuMonitor, err := gpu.NewMonitor()
	if err != nil {
		logger.Error("Failed to initialize GPU monitor", zap.Error(err))
		// Continue without GPU monitoring instead of exiting
		logger.Warn("Continuing without GPU monitoring")
	}

	// Initialize LLM client
	llmClient := llm.NewClient(cfg.Provider.ProviderType, cfg.Provider.LLMURL)

	// Initialize pricing client
	pricingClient := pricing.NewClient(cfg.Provider.URL, cfg.Provider.APIKey)

	// Register local models with pricing
	var registeredModelIDs []string
	if err := pricing.RegisterLocalModels(ctx, llmClient, pricingClient, cfg.Provider.ProviderType); err != nil {
		logger.Error("Failed to register local models", zap.Error(err))
		// Continue anyway as this is not critical
	} else {
		// Get the list of registered model IDs
		models, err := llmClient.ListModels(ctx)
		if err == nil {
			for _, model := range models.Models {
				registeredModelIDs = append(registeredModelIDs, model.ID)
			}
		}
	}

	// Initialize health reporter
	healthReporter := health.NewReporter(cfg, gpuMonitor, llmClient)

	// Initialize the registered models tracker
	healthReporter.InitializeRegisteredModels(registeredModelIDs)

	// Initialize and start HTTP server (which sets up Cloudflare tunnel)
	srv := server.CreateServer(cfg, gpuMonitor, healthReporter)

	// Start server in background and wait for Cloudflare tunnel to be ready
	serverReady := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			serverReady <- err
		}
	}()

	// Give the server a moment to establish the Cloudflare tunnel
	// The tunnel setup happens at the beginning of srv.Start()
	time.Sleep(3 * time.Second)

	// Update health reporter to use the server's Cloudflare client
	// This ensures both use the same client instance with the established tunnel
	healthReporter.SetCloudflareClient(srv.GetCloudflareClient())

	// Now start health reporting after Cloudflare tunnel is established
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		// Send initial health report (now with proper Cloudflare URL)
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

	// Check if server failed to start
	select {
	case err := <-serverReady:
		logger.Fatal("Failed to start server", zap.Error(err))
	case <-time.After(100 * time.Millisecond):
		// Server started successfully, continue
	}

	// Wait for termination signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Shutdown gracefully
	logger.Info("Shutting down gracefully...")

	// Stop the server
	if err := srv.Stop(ctx); err != nil {
		logger.Fatal("Server shutdown failed", zap.Error(err))
	}
}
