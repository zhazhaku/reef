// Package main provides the Reef distributed multi-agent orchestration system.
// It supports two modes via the --mode flag:
//
//	server — Hub node that accepts Client connections, schedules tasks,
//	         and exposes admin HTTP endpoints.
//	client — Spoke node that connects to a Server, receives tasks,
//	         and executes them via the PicoClaw AgentLoop.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhazhaku/reef/pkg/reef/server"
)

var (
	mode    = flag.String("mode", "client", "Run mode: server or client")
	version = "dev"
	commit  = "unknown"
)

func main() {
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	switch *mode {
	case "server":
		if err := runServer(logger); err != nil {
			logger.Error("server failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	case "client":
		if err := runClient(logger); err != nil {
			logger.Error("client failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q; expected 'server' or 'client'\n", *mode)
		os.Exit(1)
	}
}

func runServer(logger *slog.Logger) error {
	logger.Info("starting reef server", slog.String("version", version), slog.String("commit", commit))

	cfg := server.DefaultConfig()
	if v := os.Getenv("REEF_WS_ADDR"); v != "" {
		cfg.WebSocketAddr = v
	}
	if v := os.Getenv("REEF_ADMIN_ADDR"); v != "" {
		cfg.AdminAddr = v
	}
	if v := os.Getenv("REEF_TOKEN"); v != "" {
		cfg.Token = v
	}

	srv := server.NewServer(cfg, logger)
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Stop(); err != nil {
		logger.Error("shutdown error", slog.String("error", err.Error()))
	}
	_ = shutdownCtx
	logger.Info("server stopped")
	return nil
}

func runClient(logger *slog.Logger) error {
	logger.Info("starting reef client", slog.String("version", version), slog.String("commit", commit))
	logger.Warn("client mode requires integration with PicoClaw AgentLoop; stub implementation only")

	// TODO: Integrate with PicoClaw AgentLoop, SkillsLoader, and SwarmChannel.
	// This requires wiring:
	//   1. Load role config from skills/roles/<role>.yaml
	//   2. Initialize SkillsLoader with filtered skills
	//   3. Create MessageBus
	//   4. Create AgentLoop with SwarmChannel
	//   5. Start Connector and AgentLoop

	logger.Info("client stub running — press Ctrl+C to exit")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("client stopped")
	return nil
}
