// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/voiteco/porthook/agent/internal/agent"
)

const version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "http":
		cfg, err := parseHTTPConfig(args[1:])
		if err != nil {
			return err
		}
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		runner := agent.NewRunner(cfg, logger, os.Stdout)

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		return runner.Run(ctx)
	case "version":
		fmt.Fprintln(os.Stdout, version)
		return nil
	default:
		return usageError()
	}
}

func parseHTTPConfig(args []string) (agent.Config, error) {
	cfg := agent.ConfigFromEnv()
	cfg.AgentVersion = version

	fs := flag.NewFlagSet("http", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.ServerURL, "server", cfg.ServerURL, "gateway agent URL")
	fs.StringVar(&cfg.Token, "token", cfg.Token, "agent authentication token")
	fs.StringVar(&cfg.RequestedSubdomain, "subdomain", cfg.RequestedSubdomain, "requested public subdomain")

	var portArg string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		portArg = args[0]
		if err := fs.Parse(args[1:]); err != nil {
			return agent.Config{}, err
		}
	} else {
		if err := fs.Parse(args); err != nil {
			return agent.Config{}, err
		}
		if fs.NArg() > 0 {
			portArg = fs.Arg(0)
		}
	}

	if portArg == "" {
		return agent.Config{}, fmt.Errorf("usage: porthook http <port> [--server URL] [--token TOKEN] [--subdomain NAME]")
	}
	port, err := strconv.Atoi(portArg)
	if err != nil || port <= 0 || port > 65535 {
		return agent.Config{}, fmt.Errorf("invalid port %q", portArg)
	}
	cfg.Port = port
	cfg.LocalTarget = fmt.Sprintf("http://localhost:%d", port)

	if cfg.ServerURL == "" {
		return agent.Config{}, fmt.Errorf("server URL is required")
	}
	if cfg.Token == "" {
		return agent.Config{}, fmt.Errorf("token is required")
	}

	return cfg, nil
}

func usageError() error {
	return fmt.Errorf("usage: porthook http <port> [--server URL] [--token TOKEN] [--subdomain NAME]\n       porthook version")
}
