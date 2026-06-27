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

	"github.com/voiteco/porthook/server/gateway/internal/gateway"
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
			return fmt.Errorf("usage: porthook-gateway [version|--version]")
		}
	}

	logger := slog.New(slog.NewTextHandler(stdout, nil))
	cfg := gateway.ConfigFromEnv()

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
