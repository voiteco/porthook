// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
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

	logStore, requestLogCloser, err := openRequestLogStore(context.Background(), cfg)
	if err != nil {
		return err
	}
	if requestLogCloser != nil {
		defer requestLogCloser.Close()
	}

	server := gateway.NewServerWithRequestLogStore(cfg, logger, logStore)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("starting gateway", "version", version)

	if err := server.Run(ctx); err != nil {
		logger.Error("gateway stopped", "error", err)
		return err
	}
	return nil
}

func openRequestLogStore(ctx context.Context, cfg gateway.Config) (*gateway.PostgresRequestLogStore, io.Closer, error) {
	if cfg.RequestLogDatabaseURL == "" {
		return nil, nil, nil
	}
	db, err := sql.Open("pgx", cfg.RequestLogDatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open request log database: %w", err)
	}
	store, err := gateway.NewPostgresRequestLogStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	if err := store.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return store, db, nil
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
