// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

const defaultTokenAdminTimeout = 10 * time.Second

type tokenAdminConfig struct {
	controlPlaneURL  string
	adminToken       string
	adminTokenStdin  bool
	adminTokenIsFlag bool
	jsonOutput       bool
}

type tokenCreateConfig struct {
	tokenAdminConfig
	name   string
	scopes stringListFlag
}

type tokenRevokeConfig struct {
	tokenAdminConfig
	id string
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("scope cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

func runTokensCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return tokensUsageError()
	}

	switch args[0] {
	case "create":
		return runTokensCreate(args[1:], stdin, stdout, stderr)
	case "list":
		return runTokensList(args[1:], stdin, stdout, stderr)
	case "revoke":
		return runTokensRevoke(args[1:], stdin, stdout, stderr)
	default:
		return tokensUsageError()
	}
}

func runTokensCreate(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseTokenCreateConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg.tokenAdminConfig)
	created, err := client.createToken(context.Background(), createTokenRequest{
		Name:   cfg.name,
		Scopes: []string(cfg.scopes),
	})
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, created)
	}
	printCreatedToken(stdout, created)
	return nil
}

func runTokensList(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseTokenListConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg)
	listed, err := client.listTokens(context.Background())
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printTokenList(stdout, listed.Tokens)
	return nil
}

func runTokensRevoke(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseTokenRevokeConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg.tokenAdminConfig)
	if err := client.revokeToken(context.Background(), cfg.id); err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, map[string]any{
			"id":      cfg.id,
			"revoked": true,
		})
	}
	fmt.Fprintf(stdout, "Token revoked: %s\n", cfg.id)
	return nil
}

func parseTokenCreateConfig(args []string, stdin io.Reader, stderr io.Writer) (tokenCreateConfig, error) {
	var cfg tokenCreateConfig
	fs := newTokenAdminFlagSet("tokens create", &cfg.tokenAdminConfig, stderr)
	fs.StringVar(&cfg.name, "name", "", "token display name")
	fs.Var(&cfg.scopes, "scope", "token scope; may be repeated")
	if err := fs.Parse(args); err != nil {
		return tokenCreateConfig{}, err
	}
	if fs.NArg() > 0 {
		return tokenCreateConfig{}, fmt.Errorf("usage: porthook tokens create --control-plane URL --name NAME [--scope SCOPE] [--admin-token TOKEN | --admin-token-stdin]")
	}
	cfg.name = strings.TrimSpace(cfg.name)
	if cfg.name == "" {
		return tokenCreateConfig{}, fmt.Errorf("token name is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return tokenCreateConfig{}, err
	}
	return cfg, nil
}

func parseTokenListConfig(args []string, stdin io.Reader, stderr io.Writer) (tokenAdminConfig, error) {
	var cfg tokenAdminConfig
	fs := newTokenAdminFlagSet("tokens list", &cfg, stderr)
	if err := fs.Parse(args); err != nil {
		return tokenAdminConfig{}, err
	}
	if fs.NArg() > 0 {
		return tokenAdminConfig{}, fmt.Errorf("usage: porthook tokens list --control-plane URL [--admin-token TOKEN | --admin-token-stdin]")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg, stdin, stderr); err != nil {
		return tokenAdminConfig{}, err
	}
	return cfg, nil
}

func parseTokenRevokeConfig(args []string, stdin io.Reader, stderr io.Writer) (tokenRevokeConfig, error) {
	var cfg tokenRevokeConfig
	fs := newTokenAdminFlagSet("tokens revoke", &cfg.tokenAdminConfig, stderr)
	if err := fs.Parse(args); err != nil {
		return tokenRevokeConfig{}, err
	}
	if fs.NArg() != 1 {
		return tokenRevokeConfig{}, fmt.Errorf("usage: porthook tokens revoke --control-plane URL [--admin-token TOKEN | --admin-token-stdin] TOKEN_ID")
	}
	cfg.id = strings.TrimSpace(fs.Arg(0))
	if cfg.id == "" {
		return tokenRevokeConfig{}, fmt.Errorf("token id is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return tokenRevokeConfig{}, err
	}
	return cfg, nil
}

func newTokenAdminFlagSet(name string, cfg *tokenAdminConfig, stderr io.Writer) *flag.FlagSet {
	cfg.controlPlaneURL = strings.TrimSpace(os.Getenv("PORTHOOK_CONTROL_PLANE_URL"))
	cfg.adminToken = strings.TrimSpace(os.Getenv("PORTHOOK_CONTROL_ADMIN_TOKEN"))

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.controlPlaneURL, "control-plane", cfg.controlPlaneURL, "control-plane API URL")
	fs.StringVar(&cfg.adminToken, "admin-token", cfg.adminToken, "control-plane admin token")
	fs.BoolVar(&cfg.adminTokenStdin, "admin-token-stdin", cfg.adminTokenStdin, "read control-plane admin token from stdin")
	fs.BoolVar(&cfg.jsonOutput, "json", cfg.jsonOutput, "write JSON output")
	return fs
}

func finalizeTokenAdminConfig(fs *flag.FlagSet, cfg *tokenAdminConfig, stdin io.Reader, stderr io.Writer) error {
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "admin-token" {
			cfg.adminTokenIsFlag = true
		}
	})

	controlPlaneURL, err := normalizeControlPlaneURL(cfg.controlPlaneURL)
	if err != nil {
		return err
	}
	cfg.controlPlaneURL = controlPlaneURL

	adminToken := cfg.adminToken
	if cfg.adminTokenStdin && !cfg.adminTokenIsFlag {
		adminToken = ""
	}
	adminToken, err = readTokenInput(tokenInputConfig{
		token:         adminToken,
		tokenStdin:    cfg.adminTokenStdin,
		stdin:         stdin,
		stderr:        stderr,
		prompt:        "Admin token: ",
		name:          "admin token",
		flagName:      "--admin-token",
		stdinFlagName: "--admin-token-stdin",
	})
	if err != nil {
		return err
	}
	cfg.adminToken = adminToken
	return nil
}

func normalizeControlPlaneURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("control-plane URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid control-plane URL %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid control-plane URL %q: scheme and host are required", raw)
	}
	return strings.TrimRight(raw, "/"), nil
}

type tokenAdminClient struct {
	baseURL    string
	adminToken string
	httpClient *http.Client
}

func newTokenAdminClient(cfg tokenAdminConfig) tokenAdminClient {
	return tokenAdminClient{
		baseURL:    cfg.controlPlaneURL,
		adminToken: cfg.adminToken,
		httpClient: &http.Client{Timeout: defaultTokenAdminTimeout},
	}
}

type createTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

type createdToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
}

type tokenSummary struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type listTokensResponse struct {
	Tokens []tokenSummary `json:"tokens"`
}

func (c tokenAdminClient) createToken(ctx context.Context, req createTokenRequest) (createdToken, error) {
	var created createdToken
	if err := c.do(ctx, http.MethodPost, "/api/v1/tokens", req, &created); err != nil {
		return createdToken{}, err
	}
	return created, nil
}

func (c tokenAdminClient) listTokens(ctx context.Context) (listTokensResponse, error) {
	var listed listTokensResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/tokens", nil, &listed); err != nil {
		return listTokensResponse{}, err
	}
	return listed, nil
}

func (c tokenAdminClient) revokeToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/tokens/"+url.PathEscape(id), nil, nil)
}

func (c tokenAdminClient) do(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = &buf
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("control-plane request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(readLimitedString(resp.Body, 4096))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("control plane returned %d: %s", resp.StatusCode, message)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode control-plane response: %w", err)
	}
	return nil
}

func readLimitedString(r io.Reader, limit int64) string {
	data, _ := io.ReadAll(io.LimitReader(r, limit))
	return string(data)
}

func writeJSONOutput(w io.Writer, payload any) error {
	return json.NewEncoder(w).Encode(payload)
}

func printCreatedToken(w io.Writer, token createdToken) {
	fmt.Fprintf(w, "Created token %s\n", token.ID)
	fmt.Fprintf(w, "Name: %s\n", token.Name)
	fmt.Fprintf(w, "Scopes: %s\n", strings.Join(token.Scopes, ","))
	fmt.Fprintf(w, "Created: %s\n", token.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "Token: %s\n", token.Token)
	fmt.Fprintln(w, "The token is shown only once.")
}

func printTokenList(w io.Writer, tokens []tokenSummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSCOPES\tCREATED\tREVOKED")
	for _, token := range tokens {
		revoked := "-"
		if token.RevokedAt != nil {
			revoked = token.RevokedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\n",
			token.ID,
			token.Name,
			strings.Join(token.Scopes, ","),
			token.CreatedAt.Format(time.RFC3339),
			revoked,
		)
	}
	_ = tw.Flush()
}

func tokensUsageError() error {
	return fmt.Errorf("usage: porthook tokens create --control-plane URL --name NAME [--scope SCOPE] [--admin-token TOKEN | --admin-token-stdin]\n       porthook tokens list --control-plane URL [--admin-token TOKEN | --admin-token-stdin]\n       porthook tokens revoke --control-plane URL [--admin-token TOKEN | --admin-token-stdin] TOKEN_ID")
}
