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

const adminUsageText = `usage: porthook admin tokens <create|list|revoke> [options]
       porthook admin help`

const adminTokensUsageText = `usage: porthook admin tokens create --control-plane URL --name NAME [--scope SCOPE] [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook admin tokens list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook admin tokens revoke --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ADMIN_TOKEN_ID [--json]
       porthook admin tokens help`

const supportedAdminScopes = "admin_tokens, tokens, reservations, domains, access_policies, audit_history, runtime_diagnostics"

type adminTokenCreateConfig struct {
	tokenAdminConfig
	name   string
	scopes stringListFlag
}

type adminTokenRevokeConfig struct {
	tokenAdminConfig
	id string
}

type createAdminTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

type createdAdminToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
}

type adminTokenSummary struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type listAdminTokensResponse struct {
	Tokens []adminTokenSummary `json:"tokens"`
}

func runAdminCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return adminUsageError()
	}

	switch args[0] {
	case "tokens":
		return runAdminTokensCommand(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		printAdminUsage(stdout)
		return nil
	default:
		return adminUsageError()
	}
}

func runAdminTokensCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return adminTokensUsageError()
	}

	switch args[0] {
	case "create":
		if wantsHelp(args[1:]) {
			printAdminTokenCreateHelp(stdout)
			return nil
		}
		return runAdminTokensCreate(args[1:], stdin, stdout, stderr)
	case "list":
		if wantsHelp(args[1:]) {
			printAdminTokenListHelp(stdout)
			return nil
		}
		return runAdminTokensList(args[1:], stdin, stdout, stderr)
	case "revoke":
		if wantsHelp(args[1:]) {
			printAdminTokenRevokeHelp(stdout)
			return nil
		}
		return runAdminTokensRevoke(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		printAdminTokensUsage(stdout)
		return nil
	default:
		return adminTokensUsageError()
	}
}

func runAdminTokensCreate(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseAdminTokenCreateConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg.tokenAdminConfig)
	created, err := client.createAdminToken(context.Background(), createAdminTokenRequest{
		Name:   cfg.name,
		Scopes: []string(cfg.scopes),
	})
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, created)
	}
	printCreatedAdminToken(stdout, created)
	return nil
}

func runAdminTokensList(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseAdminTokenListConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg)
	listed, err := client.listAdminTokens(context.Background())
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printAdminTokenList(stdout, listed.Tokens)
	return nil
}

func runAdminTokensRevoke(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseAdminTokenRevokeConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg.tokenAdminConfig)
	if err := client.revokeAdminToken(context.Background(), cfg.id); err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, map[string]any{
			"id":      cfg.id,
			"revoked": true,
		})
	}
	fmt.Fprintf(stdout, "Admin token revoked: %s\n", cfg.id)
	return nil
}

func parseAdminTokenCreateConfig(args []string, stdin io.Reader, stderr io.Writer) (adminTokenCreateConfig, error) {
	var cfg adminTokenCreateConfig
	fs := newTokenAdminFlagSet("admin tokens create", &cfg.tokenAdminConfig, stderr)
	fs.StringVar(&cfg.name, "name", "", "admin token display name")
	fs.Var(&cfg.scopes, "scope", "admin token scope; may be repeated")
	if err := fs.Parse(args); err != nil {
		return adminTokenCreateConfig{}, err
	}
	if fs.NArg() > 0 {
		return adminTokenCreateConfig{}, fmt.Errorf("usage: porthook admin tokens create --control-plane URL --name NAME [--scope SCOPE] [--admin-token TOKEN | --admin-token-stdin]")
	}
	cfg.name = strings.TrimSpace(cfg.name)
	if cfg.name == "" {
		return adminTokenCreateConfig{}, fmt.Errorf("admin token name is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return adminTokenCreateConfig{}, err
	}
	return cfg, nil
}

func parseAdminTokenListConfig(args []string, stdin io.Reader, stderr io.Writer) (tokenAdminConfig, error) {
	var cfg tokenAdminConfig
	fs := newTokenAdminFlagSet("admin tokens list", &cfg, stderr)
	if err := fs.Parse(args); err != nil {
		return tokenAdminConfig{}, err
	}
	if fs.NArg() > 0 {
		return tokenAdminConfig{}, fmt.Errorf("usage: porthook admin tokens list --control-plane URL [--admin-token TOKEN | --admin-token-stdin]")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg, stdin, stderr); err != nil {
		return tokenAdminConfig{}, err
	}
	return cfg, nil
}

func parseAdminTokenRevokeConfig(args []string, stdin io.Reader, stderr io.Writer) (adminTokenRevokeConfig, error) {
	var cfg adminTokenRevokeConfig
	fs := newTokenAdminFlagSet("admin tokens revoke", &cfg.tokenAdminConfig, stderr)
	if err := fs.Parse(args); err != nil {
		return adminTokenRevokeConfig{}, err
	}
	if fs.NArg() != 1 {
		return adminTokenRevokeConfig{}, fmt.Errorf("usage: porthook admin tokens revoke --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ADMIN_TOKEN_ID")
	}
	cfg.id = strings.TrimSpace(fs.Arg(0))
	if cfg.id == "" {
		return adminTokenRevokeConfig{}, fmt.Errorf("admin token id is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return adminTokenRevokeConfig{}, err
	}
	return cfg, nil
}

func (c tokenAdminClient) createAdminToken(ctx context.Context, req createAdminTokenRequest) (createdAdminToken, error) {
	var created createdAdminToken
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin-tokens", req, &created); err != nil {
		return createdAdminToken{}, err
	}
	return created, nil
}

func (c tokenAdminClient) listAdminTokens(ctx context.Context) (listAdminTokensResponse, error) {
	var listed listAdminTokensResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/admin-tokens", nil, &listed); err != nil {
		return listAdminTokensResponse{}, err
	}
	return listed, nil
}

func (c tokenAdminClient) revokeAdminToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/admin-tokens/"+url.PathEscape(id), nil, nil)
}

func printCreatedAdminToken(w io.Writer, token createdAdminToken) {
	fmt.Fprintf(w, "Created admin token %s\n", token.ID)
	fmt.Fprintf(w, "Name: %s\n", token.Name)
	fmt.Fprintf(w, "Scopes: %s\n", strings.Join(token.Scopes, ","))
	fmt.Fprintf(w, "Created: %s\n", token.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "Token: %s\n", token.Token)
	fmt.Fprintln(w, "The admin token is shown only once.")
}

func printAdminTokenList(w io.Writer, tokens []adminTokenSummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSCOPES\tCREATED\tLAST USED\tREVOKED")
	for _, token := range tokens {
		lastUsed := "-"
		if token.LastUsedAt != nil {
			lastUsed = token.LastUsedAt.Format(time.RFC3339)
		}
		revoked := "-"
		if token.RevokedAt != nil {
			revoked = token.RevokedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			token.ID,
			token.Name,
			strings.Join(token.Scopes, ","),
			token.CreatedAt.Format(time.RFC3339),
			lastUsed,
			revoked,
		)
	}
	_ = tw.Flush()
}

func adminUsageError() error {
	return fmt.Errorf("%s", adminUsageText)
}

func adminTokensUsageError() error {
	return fmt.Errorf("%s", adminTokensUsageText)
}

func printAdminUsage(w io.Writer) {
	fmt.Fprintln(w, adminUsageText)
}

func printAdminTokensUsage(w io.Writer) {
	fmt.Fprintln(w, adminTokensUsageText)
}

func printAdminTokenCreateHelp(w io.Writer) {
	fmt.Fprintf(w, `usage: porthook admin tokens create --control-plane URL --name NAME [--scope SCOPE] [--admin-token TOKEN | --admin-token-stdin] [--json]

Create a scoped control-plane admin token. If no --scope is set, the control plane defaults to the full admin scope set.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --name NAME               Admin token display name.
  --scope SCOPE             Admin token scope; may be repeated. Supported: %s.
  --admin-token TOKEN       Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --json                    Write JSON output.
`, supportedAdminScopes)
}

func printAdminTokenListHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook admin tokens list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]

List admin token summaries without plaintext token values.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --json                    Write JSON output.`)
}

func printAdminTokenRevokeHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook admin tokens revoke --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ADMIN_TOKEN_ID [--json]

Revoke an admin token by ID.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --json                    Write JSON output.`)
}
