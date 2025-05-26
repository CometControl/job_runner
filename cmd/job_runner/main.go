package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http" // Required for http.ErrServerClosed
	"os"
	"os/signal"
	"syscall"
	"time"

	"job_runner/config"
	"job_runner/server"
)

func main() {
	// Parse command line flags
	configFile := flag.String("config", "", "Path to config file")
	httpAddr := flag.String("http.addr", "", "HTTP server address (overrides config)")
	httpPort := flag.Int("http.port", 0, "HTTP server port (overrides config)")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Override config with command line flags if provided
	if *httpAddr != "" {
		cfg.HTTPAddr = *httpAddr
	}
	if *httpPort != 0 {
		cfg.HTTPPort = *httpPort
	}

	// Create and start the server
	srv := server.New(cfg)

	// Channel to listen for OS signals
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	// Channel for server errors
	errChan := make(chan error, 1)

	go func() {
		slog.Info("Starting server...")
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start or stopped unexpectedly", "error", err)
			errChan <- err
		} else if err == http.ErrServerClosed {
			slog.Info("Server stopped gracefully (http.ErrServerClosed)")
		}
	}()

	// Block until a signal is received or server errors out
	select {
	case sig := <-stopChan:
		slog.Info("Received signal, initiating shutdown...", "signal", sig.String())
	case err := <-errChan:
		slog.Error("Server error, initiating shutdown...", "error", err)
	}

	slog.Info("Shutting down server...")

	// Create a context with a timeout for shutdown
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()

	// Attempt to gracefully shut down the server
	if err := srv.Stop(shutdownCtx); err != nil {
		slog.Error("Server shutdown failed", "error", err)
		os.Exit(1)
	} else {
		slog.Info("Server gracefully stopped")
	}
}
