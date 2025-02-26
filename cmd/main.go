package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/gpu"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/health"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/server"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize GPU monitor
	gpuMonitor, err := gpu.NewMonitor()
	if err != nil {
		log.Fatalf("Failed to initialize GPU monitor: %v", err)
	}

	// Initialize health reporter
	healthReporter := health.NewReporter(cfg, gpuMonitor)

	// Start health reporting in background
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		// Send initial health report
		if err := healthReporter.SendHealthReport(ctx); err != nil {
			log.Printf("Failed to send initial health report: %v", err)
		}

		for {
			select {
			case <-ticker.C:
				if err := healthReporter.SendHealthReport(ctx); err != nil {
					log.Printf("Failed to send health report: %v", err)
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
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for termination signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Shutdown gracefully
	fmt.Println("Shutting down gracefully...")

	// Stop the server
	if err := srv.Stop(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
}
