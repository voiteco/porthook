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
	store, err := tokenStore(context.Background(), cfg)
	if err != nil {
		return err
	}
	service := tokens.NewService(store)
	server := controlplane.NewServer(cfg, service)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return server.Run(ctx)
}

func tokenStore(ctx context.Context, cfg controlplane.Config) (tokens.Store, error) {
	if cfg.DatabaseURL == "" {
		return tokens.NewMemoryStore(), nil
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	store, err := tokens.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}
