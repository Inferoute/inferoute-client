package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/server"
	"go.uber.org/zap"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

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
		logger.Fatal("Failed to initialize GPU monitor", zap.Error(err))
	}

	// Initialize health reporter
	healthReporter := health.NewReporter(cfg, gpuMonitor)

	// Start health reporting in background
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		// Send initial health report
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

	// Initialize and start HTTP server
	srv := server.NewServer(cfg, gpuMonitor, healthReporter)
	go func() {
		if err := srv.Start(); err != nil {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

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
