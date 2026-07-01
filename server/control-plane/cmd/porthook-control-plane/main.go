// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/voiteco/porthook/server/control-plane/internal/access"
	"github.com/voiteco/porthook/server/control-plane/internal/controlplane"
	"github.com/voiteco/porthook/server/control-plane/internal/customdomains"
	"github.com/voiteco/porthook/server/control-plane/internal/reserved"
	"github.com/voiteco/porthook/server/control-plane/internal/tokens"
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
		default:
			return fmt.Errorf("usage: porthook-control-plane [version|--version]")
		}
	}

	cfg := controlplane.ConfigFromEnv()
	cfg.Version = version
	shutdownTelemetry, err := telemetry.Setup(context.Background(), telemetry.ConfigFromEnv("porthook-control-plane", version))
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(shutdownCtx)
	}()

	tokenStore, reservationStore, accessStore, customDomainStore, err := stores(context.Background(), cfg)
	if err != nil {
		return err
	}
	tokenService := tokens.NewService(tokenStore)
	reservationService := reserved.NewService(reservationStore)
	accessPolicyService := access.NewService(accessStore)
	customDomainService := customdomains.NewService(customDomainStore)
	server := controlplane.NewServerWithCustomDomains(cfg, tokenService, reservationService, accessPolicyService, customDomainService)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return server.Run(ctx)
}

func stores(ctx context.Context, cfg controlplane.Config) (tokens.Store, reserved.Store, access.Store, customdomains.Store, error) {
	if cfg.DatabaseURL == "" {
		return tokens.NewMemoryStore(), reserved.NewMemoryStore(), access.NewMemoryStore(), customdomains.NewMemoryStore(), nil
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open database: %w", err)
	}
	tokenStore, err := tokens.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	if err := tokenStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	reservationStore, err := reserved.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	if err := reservationStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	accessStore, err := access.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	if err := accessStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	customDomainStore, err := customdomains.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	if err := customDomainStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, err
	}
	return tokenStore, reservationStore, accessStore, customDomainStore, nil
}
