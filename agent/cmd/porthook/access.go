// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"
)

const accessUsageText = `usage: porthook access create --control-plane URL --reserved-subdomain-id ID --mode MODE [policy options] [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook access list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook access update --control-plane URL [--admin-token TOKEN | --admin-token-stdin] POLICY_ID [policy options] [--json]
       porthook access delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] POLICY_ID [--json]
       porthook access help`

type accessPolicyConfig struct {
	tokenAdminConfig
	mode                string
	reservedSubdomainID string
	basicUsername       string
	basicPassword       string
	basicPasswordStdin  bool
	bearerToken         string
	bearerTokenStdin    bool
	ipAllowlist         ipAllowlistFlag
}

type accessPolicyUpdateConfig struct {
	accessPolicyConfig
	id string
}

type accessPolicyDeleteConfig struct {
	tokenAdminConfig
	id string
}

type ipAllowlistFlag []string

func (f *ipAllowlistFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *ipAllowlistFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("ip allowlist entry cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

type accessPolicyRequest struct {
	ReservedSubdomainID string   `json:"reserved_subdomain_id,omitempty"`
	Mode                string   `json:"mode,omitempty"`
	BasicUsername       string   `json:"basic_username,omitempty"`
	BasicPassword       string   `json:"basic_password,omitempty"`
	BearerToken         string   `json:"bearer_token,omitempty"`
	IPAllowlist         []string `json:"ip_allowlist,omitempty"`
}

type accessPolicySummary struct {
	ID                  string    `json:"id"`
	ReservedSubdomainID string    `json:"reserved_subdomain_id"`
	Mode                string    `json:"mode"`
	BasicUsername       string    `json:"basic_username,omitempty"`
	SecretConfigured    bool      `json:"secret_configured,omitempty"`
	IPAllowlist         []string  `json:"ip_allowlist,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type listAccessPoliciesResponse struct {
	AccessPolicies []accessPolicySummary `json:"access_policies"`
}

func runAccessCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return accessUsageError()
	}

	switch args[0] {
	case "create":
		if wantsHelp(args[1:]) {
			printAccessCreateHelp(stdout)
			return nil
		}
		return runAccessCreate(args[1:], stdin, stdout, stderr)
	case "list":
		if wantsHelp(args[1:]) {
			printAccessListHelp(stdout)
			return nil
		}
		return runAccessList(args[1:], stdin, stdout, stderr)
	case "update":
		if wantsHelp(args[1:]) {
			printAccessUpdateHelp(stdout)
			return nil
		}
		return runAccessUpdate(args[1:], stdin, stdout, stderr)
	case "delete":
		if wantsHelp(args[1:]) {
			printAccessDeleteHelp(stdout)
			return nil
		}
		return runAccessDelete(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		printAccessUsage(stdout)
		return nil
	default:
		return accessUsageError()
	}
}

func runAccessCreate(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseAccessCreateConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg.tokenAdminConfig)
	created, err := client.createAccessPolicy(context.Background(), accessPolicyRequest{
		ReservedSubdomainID: cfg.reservedSubdomainID,
		Mode:                cfg.mode,
		BasicUsername:       cfg.basicUsername,
		BasicPassword:       cfg.basicPassword,
		BearerToken:         cfg.bearerToken,
		IPAllowlist:         []string(cfg.ipAllowlist),
	})
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, created)
	}
	printAccessPolicy(stdout, "Created access policy", created)
	return nil
}

func runAccessList(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseAccessListConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg)
	listed, err := client.listAccessPolicies(context.Background())
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printAccessPolicyList(stdout, listed.AccessPolicies)
	return nil
}

func runAccessUpdate(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseAccessUpdateConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg.tokenAdminConfig)
	updated, err := client.updateAccessPolicy(context.Background(), cfg.id, accessPolicyRequest{
		Mode:          cfg.mode,
		BasicUsername: cfg.basicUsername,
		BasicPassword: cfg.basicPassword,
		BearerToken:   cfg.bearerToken,
		IPAllowlist:   []string(cfg.ipAllowlist),
	})
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, updated)
	}
	printAccessPolicy(stdout, "Updated access policy", updated)
	return nil
}

func runAccessDelete(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseAccessDeleteConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg.tokenAdminConfig)
	if err := client.deleteAccessPolicy(context.Background(), cfg.id); err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, map[string]any{
			"id":      cfg.id,
			"deleted": true,
		})
	}
	fmt.Fprintf(stdout, "Access policy deleted: %s\n", cfg.id)
	return nil
}

func parseAccessCreateConfig(args []string, stdin io.Reader, stderr io.Writer) (accessPolicyConfig, error) {
	var cfg accessPolicyConfig
	fs := newAccessPolicyFlagSet("access create", &cfg, stderr)
	fs.StringVar(&cfg.reservedSubdomainID, "reserved-subdomain-id", "", "reserved subdomain ID")
	if err := fs.Parse(args); err != nil {
		return accessPolicyConfig{}, err
	}
	if fs.NArg() > 0 {
		return accessPolicyConfig{}, fmt.Errorf("usage: porthook access create --control-plane URL --reserved-subdomain-id ID --mode MODE [policy options] [--admin-token TOKEN | --admin-token-stdin]")
	}
	cfg.reservedSubdomainID = strings.TrimSpace(cfg.reservedSubdomainID)
	if cfg.reservedSubdomainID == "" {
		return accessPolicyConfig{}, fmt.Errorf("reserved subdomain ID is required")
	}
	cfg.mode = strings.TrimSpace(cfg.mode)
	if cfg.mode == "" {
		return accessPolicyConfig{}, fmt.Errorf("access policy mode is required")
	}
	if err := finalizeAccessPolicyConfig(fs, &cfg, stdin, stderr, true); err != nil {
		return accessPolicyConfig{}, err
	}
	return cfg, nil
}

func parseAccessListConfig(args []string, stdin io.Reader, stderr io.Writer) (tokenAdminConfig, error) {
	var cfg tokenAdminConfig
	fs := newTokenAdminFlagSet("access list", &cfg, stderr)
	if err := fs.Parse(args); err != nil {
		return tokenAdminConfig{}, err
	}
	if fs.NArg() > 0 {
		return tokenAdminConfig{}, fmt.Errorf("usage: porthook access list --control-plane URL [--admin-token TOKEN | --admin-token-stdin]")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg, stdin, stderr); err != nil {
		return tokenAdminConfig{}, err
	}
	return cfg, nil
}

func parseAccessUpdateConfig(args []string, stdin io.Reader, stderr io.Writer) (accessPolicyUpdateConfig, error) {
	var cfg accessPolicyUpdateConfig
	fs := newAccessPolicyFlagSet("access update", &cfg.accessPolicyConfig, stderr)
	if err := fs.Parse(args); err != nil {
		return accessPolicyUpdateConfig{}, err
	}
	if fs.NArg() != 1 {
		return accessPolicyUpdateConfig{}, fmt.Errorf("usage: porthook access update --control-plane URL [--admin-token TOKEN | --admin-token-stdin] POLICY_ID [policy options]")
	}
	cfg.id = strings.TrimSpace(fs.Arg(0))
	if cfg.id == "" {
		return accessPolicyUpdateConfig{}, fmt.Errorf("access policy ID is required")
	}
	if err := finalizeAccessPolicyConfig(fs, &cfg.accessPolicyConfig, stdin, stderr, false); err != nil {
		return accessPolicyUpdateConfig{}, err
	}
	return cfg, nil
}

func parseAccessDeleteConfig(args []string, stdin io.Reader, stderr io.Writer) (accessPolicyDeleteConfig, error) {
	var cfg accessPolicyDeleteConfig
	fs := newTokenAdminFlagSet("access delete", &cfg.tokenAdminConfig, stderr)
	if err := fs.Parse(args); err != nil {
		return accessPolicyDeleteConfig{}, err
	}
	if fs.NArg() != 1 {
		return accessPolicyDeleteConfig{}, fmt.Errorf("usage: porthook access delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] POLICY_ID")
	}
	cfg.id = strings.TrimSpace(fs.Arg(0))
	if cfg.id == "" {
		return accessPolicyDeleteConfig{}, fmt.Errorf("access policy ID is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return accessPolicyDeleteConfig{}, err
	}
	return cfg, nil
}

func newAccessPolicyFlagSet(name string, cfg *accessPolicyConfig, stderr io.Writer) *flag.FlagSet {
	fs := newTokenAdminFlagSet(name, &cfg.tokenAdminConfig, stderr)
	fs.StringVar(&cfg.mode, "mode", "", "access policy mode: public, basic_auth, bearer_token, or ip_allowlist")
	fs.StringVar(&cfg.basicUsername, "basic-username", "", "basic auth username")
	fs.StringVar(&cfg.basicPassword, "basic-password", "", "basic auth password")
	fs.BoolVar(&cfg.basicPasswordStdin, "basic-password-stdin", cfg.basicPasswordStdin, "read basic auth password from stdin")
	fs.StringVar(&cfg.bearerToken, "bearer-token", "", "bearer token accepted by the gateway")
	fs.BoolVar(&cfg.bearerTokenStdin, "bearer-token-stdin", cfg.bearerTokenStdin, "read bearer token from stdin")
	fs.Var(&cfg.ipAllowlist, "ip-allowlist", "allowed IP address or CIDR range; may be repeated")
	return fs
}

func finalizeAccessPolicyConfig(fs *flag.FlagSet, cfg *accessPolicyConfig, stdin io.Reader, stderr io.Writer, requireMode bool) error {
	if strings.TrimSpace(cfg.basicPassword) != "" && cfg.basicPasswordStdin {
		return fmt.Errorf("--basic-password and --basic-password-stdin are mutually exclusive")
	}
	if strings.TrimSpace(cfg.bearerToken) != "" && cfg.bearerTokenStdin {
		return fmt.Errorf("--bearer-token and --bearer-token-stdin are mutually exclusive")
	}
	if cfg.basicPasswordStdin && cfg.bearerTokenStdin {
		return fmt.Errorf("--basic-password-stdin and --bearer-token-stdin are mutually exclusive")
	}
	if cfg.adminTokenStdin && (cfg.basicPasswordStdin || cfg.bearerTokenStdin) {
		return fmt.Errorf("--admin-token-stdin cannot be combined with policy secret stdin flags")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return err
	}

	cfg.mode = strings.TrimSpace(cfg.mode)
	if requireMode && cfg.mode == "" {
		return fmt.Errorf("access policy mode is required")
	}
	cfg.basicUsername = strings.TrimSpace(cfg.basicUsername)
	if cfg.basicPasswordStdin {
		password, err := readTokenInput(tokenInputConfig{
			tokenStdin:    true,
			stdin:         stdin,
			stderr:        stderr,
			prompt:        "Basic password: ",
			name:          "basic password",
			flagName:      "--basic-password",
			stdinFlagName: "--basic-password-stdin",
		})
		if err != nil {
			return err
		}
		cfg.basicPassword = password
	} else {
		cfg.basicPassword = strings.TrimSpace(cfg.basicPassword)
	}
	if cfg.bearerTokenStdin {
		token, err := readTokenInput(tokenInputConfig{
			tokenStdin:    true,
			stdin:         stdin,
			stderr:        stderr,
			prompt:        "Bearer token: ",
			name:          "bearer token",
			flagName:      "--bearer-token",
			stdinFlagName: "--bearer-token-stdin",
		})
		if err != nil {
			return err
		}
		cfg.bearerToken = token
	} else {
		cfg.bearerToken = strings.TrimSpace(cfg.bearerToken)
	}
	return nil
}

func (c tokenAdminClient) createAccessPolicy(ctx context.Context, req accessPolicyRequest) (accessPolicySummary, error) {
	var created accessPolicySummary
	if err := c.do(ctx, http.MethodPost, "/api/v1/access-policies", req, &created); err != nil {
		return accessPolicySummary{}, err
	}
	return created, nil
}

func (c tokenAdminClient) listAccessPolicies(ctx context.Context) (listAccessPoliciesResponse, error) {
	var listed listAccessPoliciesResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/access-policies", nil, &listed); err != nil {
		return listAccessPoliciesResponse{}, err
	}
	return listed, nil
}

func (c tokenAdminClient) updateAccessPolicy(ctx context.Context, id string, req accessPolicyRequest) (accessPolicySummary, error) {
	var updated accessPolicySummary
	if err := c.do(ctx, http.MethodPut, "/api/v1/access-policies/"+url.PathEscape(id), req, &updated); err != nil {
		return accessPolicySummary{}, err
	}
	return updated, nil
}

func (c tokenAdminClient) deleteAccessPolicy(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/access-policies/"+url.PathEscape(id), nil, nil)
}

func printAccessPolicy(w io.Writer, prefix string, policy accessPolicySummary) {
	fmt.Fprintf(w, "%s %s\n", prefix, policy.ID)
	fmt.Fprintf(w, "Reserved subdomain ID: %s\n", policy.ReservedSubdomainID)
	fmt.Fprintf(w, "Mode: %s\n", policy.Mode)
	if policy.BasicUsername != "" {
		fmt.Fprintf(w, "Basic username: %s\n", policy.BasicUsername)
	}
	if len(policy.IPAllowlist) > 0 {
		fmt.Fprintf(w, "IP allowlist: %s\n", strings.Join(policy.IPAllowlist, ","))
	}
	fmt.Fprintf(w, "Secret configured: %t\n", policy.SecretConfigured)
	fmt.Fprintf(w, "Updated: %s\n", policy.UpdatedAt.Format(time.RFC3339))
}

func printAccessPolicyList(w io.Writer, policies []accessPolicySummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tRESERVED SUBDOMAIN ID\tMODE\tBASIC USERNAME\tSECRET\tIP ALLOWLIST\tUPDATED")
	for _, policy := range policies {
		username := policy.BasicUsername
		if username == "" {
			username = "-"
		}
		ipAllowlist := "-"
		if len(policy.IPAllowlist) > 0 {
			ipAllowlist = strings.Join(policy.IPAllowlist, ",")
		}
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%t\t%s\t%s\n",
			policy.ID,
			policy.ReservedSubdomainID,
			policy.Mode,
			username,
			policy.SecretConfigured,
			ipAllowlist,
			policy.UpdatedAt.Format(time.RFC3339),
		)
	}
	_ = tw.Flush()
}

func accessUsageError() error {
	return fmt.Errorf("%s", accessUsageText)
}

func printAccessUsage(w io.Writer) {
	fmt.Fprintln(w, accessUsageText)
}

func printAccessCreateHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook access create --control-plane URL --reserved-subdomain-id ID --mode MODE [policy options] [--admin-token TOKEN | --admin-token-stdin] [--json]

Create an access policy for a reserved subdomain.

Policy options:
  --mode MODE                public, basic_auth, bearer_token, or ip_allowlist.
  --basic-username USER      Username for basic_auth mode.
  --basic-password PASS      Password for basic_auth mode. Prefer --basic-password-stdin.
  --basic-password-stdin     Read the basic auth password from stdin.
  --bearer-token TOKEN       Bearer token for bearer_token mode. Prefer --bearer-token-stdin.
  --bearer-token-stdin       Read the bearer token from stdin.
  --ip-allowlist VALUE       Allowed IP address or CIDR range; may be repeated.

Options:
  --control-plane URL        Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --reserved-subdomain-id ID Reserved subdomain ID.
  --admin-token TOKEN        Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin        Read the control-plane admin token from stdin.
  --json                     Write JSON output.`)
}

func printAccessListHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook access list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]

List access policies without plaintext policy secrets.

Options:
  --control-plane URL        Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN        Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin        Read the control-plane admin token from stdin.
  --json                     Write JSON output.`)
}

func printAccessUpdateHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook access update --control-plane URL [--admin-token TOKEN | --admin-token-stdin] POLICY_ID [policy options] [--json]

Update an access policy. Omitted secret values are preserved when the mode stays unchanged.

Policy options:
  --mode MODE                public, basic_auth, bearer_token, or ip_allowlist.
  --basic-username USER      Username for basic_auth mode.
  --basic-password PASS      Password for basic_auth mode. Prefer --basic-password-stdin.
  --basic-password-stdin     Read the basic auth password from stdin.
  --bearer-token TOKEN       Bearer token for bearer_token mode. Prefer --bearer-token-stdin.
  --bearer-token-stdin       Read the bearer token from stdin.
  --ip-allowlist VALUE       Allowed IP address or CIDR range; may be repeated.

Options:
  --control-plane URL        Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN        Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin        Read the control-plane admin token from stdin.
  --json                     Write JSON output.`)
}

func printAccessDeleteHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook access delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] POLICY_ID [--json]

Delete an access policy by ID.

Options:
  --control-plane URL        Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN        Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin        Read the control-plane admin token from stdin.
  --json                     Write JSON output.`)
}
