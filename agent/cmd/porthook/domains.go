// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"
)

const domainsUsageText = `usage: porthook domains create --control-plane URL --hostname HOSTNAME --reserved-subdomain-id ID [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook domains list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook domains delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ID_OR_HOSTNAME [--json]
       porthook domains help`

type domainCreateConfig struct {
	tokenAdminConfig
	hostname            string
	reservedSubdomainID string
}

type domainDeleteConfig struct {
	tokenAdminConfig
	target string
}

type createCustomDomainRequest struct {
	Hostname            string `json:"hostname"`
	ReservedSubdomainID string `json:"reserved_subdomain_id"`
}

type customDomainSummary struct {
	ID                  string    `json:"id"`
	Hostname            string    `json:"hostname"`
	ReservedSubdomainID string    `json:"reserved_subdomain_id"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type listCustomDomainsResponse struct {
	CustomDomains []customDomainSummary `json:"custom_domains"`
}

func runDomainsCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return domainsUsageError()
	}

	switch args[0] {
	case "create":
		if wantsHelp(args[1:]) {
			printDomainCreateHelp(stdout)
			return nil
		}
		return runDomainCreate(args[1:], stdin, stdout, stderr)
	case "list":
		if wantsHelp(args[1:]) {
			printDomainListHelp(stdout)
			return nil
		}
		return runDomainList(args[1:], stdin, stdout, stderr)
	case "delete":
		if wantsHelp(args[1:]) {
			printDomainDeleteHelp(stdout)
			return nil
		}
		return runDomainDelete(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		printDomainsUsage(stdout)
		return nil
	default:
		return domainsUsageError()
	}
}

func runDomainCreate(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseDomainCreateConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg.tokenAdminConfig)
	created, err := client.createCustomDomain(context.Background(), createCustomDomainRequest{
		Hostname:            cfg.hostname,
		ReservedSubdomainID: cfg.reservedSubdomainID,
	})
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, created)
	}
	printCustomDomain(stdout, "Created custom domain", created)
	return nil
}

func runDomainList(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseDomainListConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg)
	listed, err := client.listCustomDomains(context.Background())
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printCustomDomainList(stdout, listed.CustomDomains)
	return nil
}

func runDomainDelete(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseDomainDeleteConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg.tokenAdminConfig)
	deleted, err := client.resolveCustomDomainDeleteTarget(context.Background(), cfg.target)
	if err != nil {
		return err
	}
	if err := client.deleteCustomDomain(context.Background(), deleted.ID); err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, map[string]any{
			"id":       deleted.ID,
			"hostname": deleted.Hostname,
			"deleted":  true,
		})
	}
	if deleted.Hostname != "" {
		fmt.Fprintf(stdout, "Custom domain deleted: %s (%s)\n", deleted.Hostname, deleted.ID)
		return nil
	}
	fmt.Fprintf(stdout, "Custom domain deleted: %s\n", deleted.ID)
	return nil
}

func parseDomainCreateConfig(args []string, stdin io.Reader, stderr io.Writer) (domainCreateConfig, error) {
	var cfg domainCreateConfig
	fs := newTokenAdminFlagSet("domains create", &cfg.tokenAdminConfig, stderr)
	fs.StringVar(&cfg.hostname, "hostname", "", "custom domain hostname")
	fs.StringVar(&cfg.reservedSubdomainID, "reserved-subdomain-id", "", "reserved subdomain ID")
	if err := fs.Parse(args); err != nil {
		return domainCreateConfig{}, err
	}
	if fs.NArg() > 0 {
		return domainCreateConfig{}, fmt.Errorf("usage: porthook domains create --control-plane URL --hostname HOSTNAME --reserved-subdomain-id ID [--admin-token TOKEN | --admin-token-stdin]")
	}
	cfg.hostname = strings.TrimSpace(cfg.hostname)
	if cfg.hostname == "" {
		return domainCreateConfig{}, fmt.Errorf("custom domain hostname is required")
	}
	cfg.reservedSubdomainID = strings.TrimSpace(cfg.reservedSubdomainID)
	if cfg.reservedSubdomainID == "" {
		return domainCreateConfig{}, fmt.Errorf("reserved subdomain ID is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return domainCreateConfig{}, err
	}
	return cfg, nil
}

func parseDomainListConfig(args []string, stdin io.Reader, stderr io.Writer) (tokenAdminConfig, error) {
	var cfg tokenAdminConfig
	fs := newTokenAdminFlagSet("domains list", &cfg, stderr)
	if err := fs.Parse(args); err != nil {
		return tokenAdminConfig{}, err
	}
	if fs.NArg() > 0 {
		return tokenAdminConfig{}, fmt.Errorf("usage: porthook domains list --control-plane URL [--admin-token TOKEN | --admin-token-stdin]")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg, stdin, stderr); err != nil {
		return tokenAdminConfig{}, err
	}
	return cfg, nil
}

func parseDomainDeleteConfig(args []string, stdin io.Reader, stderr io.Writer) (domainDeleteConfig, error) {
	var cfg domainDeleteConfig
	fs := newTokenAdminFlagSet("domains delete", &cfg.tokenAdminConfig, stderr)
	if err := fs.Parse(args); err != nil {
		return domainDeleteConfig{}, err
	}
	if fs.NArg() != 1 {
		return domainDeleteConfig{}, fmt.Errorf("usage: porthook domains delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ID_OR_HOSTNAME")
	}
	cfg.target = strings.TrimSpace(fs.Arg(0))
	if cfg.target == "" {
		return domainDeleteConfig{}, fmt.Errorf("custom domain id or hostname is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return domainDeleteConfig{}, err
	}
	return cfg, nil
}

func (c tokenAdminClient) createCustomDomain(ctx context.Context, req createCustomDomainRequest) (customDomainSummary, error) {
	var created customDomainSummary
	if err := c.do(ctx, http.MethodPost, "/api/v1/custom-domains", req, &created); err != nil {
		return customDomainSummary{}, err
	}
	return created, nil
}

func (c tokenAdminClient) listCustomDomains(ctx context.Context) (listCustomDomainsResponse, error) {
	var listed listCustomDomainsResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/custom-domains", nil, &listed); err != nil {
		return listCustomDomainsResponse{}, err
	}
	return listed, nil
}

func (c tokenAdminClient) deleteCustomDomain(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/custom-domains/"+url.PathEscape(id), nil, nil)
}

func (c tokenAdminClient) resolveCustomDomainDeleteTarget(ctx context.Context, target string) (customDomainSummary, error) {
	if strings.HasPrefix(target, "cd_") {
		return customDomainSummary{ID: target}, nil
	}

	listed, err := c.listCustomDomains(ctx)
	if err != nil {
		return customDomainSummary{}, err
	}
	hostname := normalizeDomainTarget(target)
	for _, domain := range listed.CustomDomains {
		if domain.Hostname == hostname {
			return domain, nil
		}
	}
	return customDomainSummary{}, fmt.Errorf("custom domain %q was not found", target)
}

func normalizeDomainTarget(target string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(target)), ".")
}

func printCustomDomain(w io.Writer, prefix string, domain customDomainSummary) {
	fmt.Fprintf(w, "%s %s\n", prefix, domain.ID)
	fmt.Fprintf(w, "Hostname: %s\n", domain.Hostname)
	fmt.Fprintf(w, "Reserved subdomain ID: %s\n", domain.ReservedSubdomainID)
	fmt.Fprintf(w, "Status: %s\n", domain.Status)
	fmt.Fprintf(w, "Updated: %s\n", domain.UpdatedAt.Format(time.RFC3339))
}

func printCustomDomainList(w io.Writer, domains []customDomainSummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tHOSTNAME\tRESERVED SUBDOMAIN ID\tSTATUS\tUPDATED")
	for _, domain := range domains {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\n",
			domain.ID,
			domain.Hostname,
			domain.ReservedSubdomainID,
			domain.Status,
			domain.UpdatedAt.Format(time.RFC3339),
		)
	}
	_ = tw.Flush()
}

func domainsUsageError() error {
	return fmt.Errorf("%s", domainsUsageText)
}

func printDomainsUsage(w io.Writer) {
	fmt.Fprintln(w, domainsUsageText)
}

func printDomainCreateHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook domains create --control-plane URL --hostname HOSTNAME --reserved-subdomain-id ID [--admin-token TOKEN | --admin-token-stdin] [--json]

Create a custom domain mapping for a reserved subdomain.

Options:
  --control-plane URL        Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --hostname HOSTNAME        Custom domain hostname.
  --reserved-subdomain-id ID Reserved subdomain ID.
  --admin-token TOKEN        Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin        Read the control-plane admin token from stdin.
  --json                     Write JSON output.`)
}

func printDomainListHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook domains list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]

List custom domain mappings.

Options:
  --control-plane URL        Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN        Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin        Read the control-plane admin token from stdin.
  --json                     Write JSON output.`)
}

func printDomainDeleteHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook domains delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ID_OR_HOSTNAME [--json]

Delete a custom domain mapping by custom domain ID or hostname.

Options:
  --control-plane URL        Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN        Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin        Read the control-plane admin token from stdin.
  --json                     Write JSON output.`)
}
