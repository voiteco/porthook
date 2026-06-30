// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/voiteco/porthook/agent/internal/agent"
	"github.com/voiteco/porthook/protocol/names"
	"golang.org/x/term"
)

var version = "dev"

func main() {
	if err := runWithIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithIO(args, os.Stdin, os.Stdout, os.Stderr)
}

func runWithIO(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "http":
		cfg, err := parseHTTPConfig(args[1:])
		if err != nil {
			return err
		}
		logger := slog.New(slog.NewTextHandler(stderr, nil))
		runner := agent.NewRunner(cfg, logger, stdout)

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		return runner.Run(ctx)
	case "login":
		cfg, err := parseLoginConfig(args[1:], stdin, stderr)
		if err != nil {
			return err
		}
		if err := agent.SaveConfigFile(cfg); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Login saved")
		return nil
	case "logout":
		if len(args) > 1 {
			return usageError()
		}
		if err := agent.RemoveConfigFile(); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Login removed")
		return nil
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, version)
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
	if cfg.RequestedSubdomain != "" {
		if err := names.ValidateSubdomain(cfg.RequestedSubdomain); err != nil {
			return agent.Config{}, fmt.Errorf("invalid subdomain %q: %w", cfg.RequestedSubdomain, err)
		}
	}

	return cfg, nil
}

func parseLoginConfig(args []string, stdin io.Reader, stderr io.Writer) (agent.ConfigFile, error) {
	defaults := agent.ConfigFromEnv()
	serverURL := defaults.ServerURL
	var token string
	var tokenStdin bool

	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&serverURL, "server", serverURL, "gateway agent URL")
	fs.StringVar(&token, "token", token, "agent authentication token")
	fs.BoolVar(&tokenStdin, "token-stdin", tokenStdin, "read agent authentication token from stdin")
	if err := fs.Parse(args); err != nil {
		return agent.ConfigFile{}, err
	}
	if fs.NArg() > 0 {
		return agent.ConfigFile{}, fmt.Errorf("usage: porthook login --server URL [--token TOKEN | --token-stdin]")
	}
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return agent.ConfigFile{}, fmt.Errorf("server URL is required")
	}
	token, err := readLoginToken(token, tokenStdin, stdin, stderr)
	if err != nil {
		return agent.ConfigFile{}, err
	}
	return agent.ConfigFile{
		ServerURL: serverURL,
		Token:     token,
	}, nil
}

func readLoginToken(token string, tokenStdin bool, stdin io.Reader, stderr io.Writer) (string, error) {
	token = strings.TrimSpace(token)
	if token != "" && tokenStdin {
		return "", fmt.Errorf("--token and --token-stdin are mutually exclusive")
	}
	if token != "" {
		return token, nil
	}
	if tokenStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read token from stdin: %w", err)
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("token from stdin is empty")
		}
		return token, nil
	}
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprint(stderr, "Token: ")
		data, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(stderr)
		if err != nil {
			return "", fmt.Errorf("read token from terminal: %w", err)
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("token is required")
		}
		return token, nil
	}
	return "", fmt.Errorf("token is required; pass --token or pipe it with --token-stdin")
}

func usageError() error {
	return fmt.Errorf("usage: porthook login --server URL [--token TOKEN | --token-stdin]\n       porthook logout\n       porthook http <port> [--server URL] [--token TOKEN] [--subdomain NAME]\n       porthook version")
}
