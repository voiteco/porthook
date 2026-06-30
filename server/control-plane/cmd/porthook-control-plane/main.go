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

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/voiteco/porthook/server/control-plane/internal/controlplane"
	"github.com/voiteco/porthook/server/control-plane/internal/reserved"
	"github.com/voiteco/porthook/server/control-plane/internal/tokens"
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
	tokenStore, reservationStore, err := stores(context.Background(), cfg)
	if err != nil {
		return err
	}
	tokenService := tokens.NewService(tokenStore)
	reservationService := reserved.NewService(reservationStore)
	server := controlplane.NewServer(cfg, tokenService, reservationService)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return server.Run(ctx)
}

func stores(ctx context.Context, cfg controlplane.Config) (tokens.Store, reserved.Store, error) {
	if cfg.DatabaseURL == "" {
		return tokens.NewMemoryStore(), reserved.NewMemoryStore(), nil
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	tokenStore, err := tokens.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	if err := tokenStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	reservationStore, err := reserved.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	if err := reservationStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return tokenStore, reservationStore, nil
}
