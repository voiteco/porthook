// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voiteco/porthook/server/gateway/internal/gateway"
	"github.com/voiteco/porthook/server/internal/healthcheck"
	"github.com/voiteco/porthook/server/internal/telemetry"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-version":
			fmt.Fprintln(stdout, version)
			return nil
		case "healthcheck":
			if len(args) > 1 {
				return fmt.Errorf("usage: porthook-gateway [version|--version|healthcheck]")
			}
			return runHealthcheck(context.Background(), stdout)
		default:
			return fmt.Errorf("usage: porthook-gateway [version|--version|healthcheck]")
		}
	}

	logger := slog.New(slog.NewTextHandler(stdout, nil))
	cfg := gateway.ConfigFromEnv()

	shutdownTelemetry, err := telemetry.Setup(context.Background(), telemetry.ConfigFromEnv("porthook-gateway", version))
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), maxDuration(cfg.ShutdownTimeout, 5*time.Second))
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	server := gateway.NewServer(cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("starting gateway", "version", version)

	if err := server.Run(ctx); err != nil {
		logger.Error("gateway stopped", "error", err)
		return err
	}
	return nil
}

func runHealthcheck(ctx context.Context, stdout io.Writer) error {
	cfg := gateway.ConfigFromEnv()
	healthcheckURL, err := healthcheck.URLFromEnvOrListenAddr(cfg.PublicAddr, "/readyz")
	if err != nil {
		return err
	}
	if err := healthcheck.HTTP(ctx, healthcheckURL, healthcheck.TimeoutFromEnv()); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
