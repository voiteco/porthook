// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/voiteco/porthook/server/control-plane/internal/access"
	"github.com/voiteco/porthook/server/control-plane/internal/admintokens"
	"github.com/voiteco/porthook/server/control-plane/internal/controlplane"
	"github.com/voiteco/porthook/server/control-plane/internal/customdomains"
	"github.com/voiteco/porthook/server/control-plane/internal/reserved"
	"github.com/voiteco/porthook/server/control-plane/internal/tokens"
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
				return fmt.Errorf("usage: porthook-control-plane [version|--version|healthcheck|configcheck]")
			}
			return runHealthcheck(context.Background(), stdout)
		case "configcheck":
			return runConfigCheck(args[1:], stdout)
		default:
			return fmt.Errorf("usage: porthook-control-plane [version|--version|healthcheck|configcheck]")
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

	tokenStore, adminTokenStore, reservationStore, accessStore, customDomainStore, auditEventStore, err := stores(context.Background(), cfg)
	if err != nil {
		return err
	}
	tokenService := tokens.NewService(tokenStore)
	adminTokenService := admintokens.NewService(adminTokenStore)
	reservationService := reserved.NewService(reservationStore)
	accessPolicyService := access.NewService(accessStore)
	customDomainService := customdomains.NewServiceWithResolver(customDomainStore, customdomains.NewTXTResolver(cfg.DNSResolverAddr))
	server := controlplane.NewServerWithAdminTokens(cfg, tokenService, reservationService, accessPolicyService, customDomainService, auditEventStore, adminTokenService)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return server.Run(ctx)
}

func runConfigCheck(args []string, stdout io.Writer) error {
	var production bool
	fs := flag.NewFlagSet("configcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&production, "production", false, "validate production deployment requirements")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: porthook-control-plane configcheck [--production]")
	}

	report := controlplane.ValidateConfig(controlplane.ConfigFromEnv(), controlplane.ConfigValidationOptions{
		Production: production,
	})
	printControlPlaneConfigValidationReport(stdout, report)
	if report.HasErrors() {
		return fmt.Errorf("control-plane configuration check failed")
	}
	return nil
}

func printControlPlaneConfigValidationReport(stdout io.Writer, report controlplane.ConfigValidationReport) {
	for _, warning := range report.Warnings {
		fmt.Fprintf(stdout, "warning: %s: %s\n", warning.Field, warning.Message)
	}
	for _, issue := range report.Errors {
		fmt.Fprintf(stdout, "error: %s: %s\n", issue.Field, issue.Message)
	}
	if !report.HasErrors() {
		fmt.Fprintln(stdout, "ok")
	}
}

func runHealthcheck(ctx context.Context, stdout io.Writer) error {
	cfg := controlplane.ConfigFromEnv()
	healthcheckURL, err := healthcheck.URLFromEnvOrListenAddr(cfg.Addr, "/readyz")
	if err != nil {
		return err
	}
	if err := healthcheck.HTTP(ctx, healthcheckURL, healthcheck.TimeoutFromEnv()); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

func stores(ctx context.Context, cfg controlplane.Config) (tokens.Store, admintokens.Store, reserved.Store, access.Store, customdomains.Store, controlplane.AuditEventStore, error) {
	if cfg.DatabaseURL == "" {
		return tokens.NewMemoryStore(), admintokens.NewMemoryStore(), reserved.NewMemoryStore(), access.NewMemoryStore(), customdomains.NewMemoryStore(), controlplane.NewMemoryAuditEventStore(500), nil
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("open database: %w", err)
	}
	tokenStore, err := tokens.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := tokenStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	reservationStore, err := reserved.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := reservationStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	accessStore, err := access.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := accessStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	customDomainStore, err := customdomains.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := customDomainStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	auditEventStore, err := controlplane.NewPostgresAuditEventStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := auditEventStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	adminTokenStore, err := admintokens.NewPostgresStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	if err := adminTokenStore.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, nil, nil, nil, nil, err
	}
	return tokenStore, adminTokenStore, reservationStore, accessStore, customDomainStore, auditEventStore, nil
}
