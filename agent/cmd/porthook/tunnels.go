// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"
)

const defaultTunnelAPITimeout = 10 * time.Second

const tunnelsUsageText = `usage: porthook tunnels list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] [--json]
       porthook tunnels show --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] TUNNEL_ID [--json]
       porthook tunnels help`

type tunnelCLIConfig struct {
	tokenAdminConfig
	timeout time.Duration
}

type tunnelShowConfig struct {
	tunnelCLIConfig
	id string
}

type tunnelSummaryCLI struct {
	TunnelID    string    `json:"tunnel_id"`
	Subdomain   string    `json:"subdomain"`
	PublicURL   string    `json:"public_url"`
	Protocol    string    `json:"protocol"`
	ConnectedAt time.Time `json:"connected_at"`
}

type listTunnelsResponse struct {
	Tunnels []tunnelSummaryCLI `json:"tunnels"`
}

type tunnelDetailCLI struct {
	TunnelID         string              `json:"tunnel_id"`
	Subdomain        string              `json:"subdomain"`
	PublicURL        string              `json:"public_url"`
	Protocol         string              `json:"protocol"`
	AgentVersion     string              `json:"agent_version,omitempty"`
	ProtocolVersion  string              `json:"protocol_version,omitempty"`
	ConnectedAt      time.Time           `json:"connected_at"`
	ConnectedSeconds int64               `json:"connected_seconds"`
	ActiveStreams    int                 `json:"active_streams"`
	StreamCapacity   int                 `json:"stream_capacity"`
	RecentRequests   tunnelRequestRecent `json:"recent_requests"`
}

type tunnelRequestRecent struct {
	Count         int        `json:"count"`
	LastRequestAt *time.Time `json:"last_request_at,omitempty"`
	LastStatus    int        `json:"last_status,omitempty"`
	LastOutcome   string     `json:"last_outcome,omitempty"`
	LastRequestID string     `json:"last_request_id,omitempty"`
	ErrorCount    int        `json:"error_count"`
	CustomDomains []string   `json:"custom_domains,omitempty"`
}

type showTunnelResponse struct {
	Tunnel tunnelDetailCLI `json:"tunnel"`
}

type tunnelAPIClient struct {
	baseURL    string
	apiPrefix  string
	adminToken string
	client     *http.Client
}

func runTunnelsCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return tunnelsUsageError()
	}

	switch args[0] {
	case "list":
		if wantsHelp(args[1:]) {
			printTunnelListHelp(stdout)
			return nil
		}
		return runTunnelsList(args[1:], stdin, stdout, stderr)
	case "show":
		if wantsHelp(args[1:]) {
			printTunnelShowHelp(stdout)
			return nil
		}
		return runTunnelsShow(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		printTunnelsUsage(stdout)
		return nil
	default:
		return tunnelsUsageError()
	}
}

func runTunnelsList(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseTunnelListConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTunnelAPIClient(cfg)
	listed, err := client.listTunnels(context.Background())
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printTunnelList(stdout, listed.Tunnels)
	return nil
}

func runTunnelsShow(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseTunnelShowConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTunnelAPIClient(cfg.tunnelCLIConfig)
	shown, err := client.showTunnel(context.Background(), cfg.id)
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, shown)
	}
	printTunnelDetail(stdout, shown.Tunnel)
	return nil
}

func parseTunnelListConfig(args []string, stdin io.Reader, stderr io.Writer) (tunnelCLIConfig, error) {
	cfg := defaultTunnelCLIConfig()
	fs := newTunnelFlagSet("tunnels list", &cfg, stderr)
	if err := fs.Parse(args); err != nil {
		return tunnelCLIConfig{}, err
	}
	if fs.NArg() > 0 {
		return tunnelCLIConfig{}, fmt.Errorf("usage: porthook tunnels list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] [--json]")
	}
	if err := finalizeTunnelCLIConfig(fs, &cfg, stdin, stderr); err != nil {
		return tunnelCLIConfig{}, err
	}
	return cfg, nil
}

func parseTunnelShowConfig(args []string, stdin io.Reader, stderr io.Writer) (tunnelShowConfig, error) {
	cfg := tunnelShowConfig{tunnelCLIConfig: defaultTunnelCLIConfig()}
	fs := newTunnelFlagSet("tunnels show", &cfg.tunnelCLIConfig, stderr)
	if err := fs.Parse(args); err != nil {
		return tunnelShowConfig{}, err
	}
	if fs.NArg() != 1 {
		return tunnelShowConfig{}, fmt.Errorf("usage: porthook tunnels show --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] TUNNEL_ID [--json]")
	}
	cfg.id = strings.TrimSpace(fs.Arg(0))
	if cfg.id == "" {
		return tunnelShowConfig{}, fmt.Errorf("tunnel id is required")
	}
	if err := finalizeTunnelCLIConfig(fs, &cfg.tunnelCLIConfig, stdin, stderr); err != nil {
		return tunnelShowConfig{}, err
	}
	return cfg, nil
}

func defaultTunnelCLIConfig() tunnelCLIConfig {
	return tunnelCLIConfig{
		timeout: defaultTunnelAPITimeout,
	}
}

func newTunnelFlagSet(name string, cfg *tunnelCLIConfig, stderr io.Writer) *flag.FlagSet {
	fs := newTokenAdminFlagSet(name, &cfg.tokenAdminConfig, stderr)
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "HTTP timeout")
	return fs
}

func finalizeTunnelCLIConfig(fs *flag.FlagSet, cfg *tunnelCLIConfig, stdin io.Reader, stderr io.Writer) error {
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return err
	}
	if cfg.timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}

func newTunnelAPIClient(cfg tunnelCLIConfig) tunnelAPIClient {
	return tunnelAPIClient{
		baseURL:    cfg.controlPlaneURL,
		apiPrefix:  "/api/v1/gateway",
		adminToken: cfg.adminToken,
		client:     &http.Client{Timeout: cfg.timeout},
	}
}

func newDirectTunnelAPIClient(baseURL string, timeout time.Duration) tunnelAPIClient {
	return tunnelAPIClient{
		baseURL:   baseURL,
		apiPrefix: "/api/v1",
		client:    &http.Client{Timeout: timeout},
	}
}

func (c tunnelAPIClient) listTunnels(ctx context.Context) (listTunnelsResponse, error) {
	var listed listTunnelsResponse
	if err := c.do(ctx, http.MethodGet, c.apiPrefix+"/tunnels", &listed); err != nil {
		return listTunnelsResponse{}, err
	}
	return listed, nil
}

func (c tunnelAPIClient) showTunnel(ctx context.Context, id string) (showTunnelResponse, error) {
	var shown showTunnelResponse
	if err := c.do(ctx, http.MethodGet, c.apiPrefix+"/tunnels/"+url.PathEscape(id), &shown); err != nil {
		return showTunnelResponse{}, err
	}
	return shown, nil
}

func (c tunnelAPIClient) do(ctx context.Context, method string, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	}
	req.Header.Set("User-Agent", "porthook/"+version+" tunnels")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return gatewayAPIStatusError(resp.StatusCode, readLimitedString(resp.Body, 4096))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode gateway response: %w", err)
	}
	return nil
}

func gatewayAPIStatusError(status int, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("gateway returned status %d: %s", status, message)
}

func printTunnelList(w io.Writer, tunnels []tunnelSummaryCLI) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SUBDOMAIN\tTUNNEL ID\tPUBLIC URL\tPROTOCOL\tCONNECTED")
	for _, tunnel := range tunnels {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\n",
			tunnel.Subdomain,
			tunnel.TunnelID,
			tunnel.PublicURL,
			defaultString(tunnel.Protocol, "http"),
			formatTunnelTime(tunnel.ConnectedAt),
		)
	}
	_ = tw.Flush()
}

func printTunnelDetail(w io.Writer, tunnel tunnelDetailCLI) {
	recent := tunnel.RecentRequests
	fmt.Fprintf(w, "Tunnel: %s\n", tunnel.TunnelID)
	fmt.Fprintf(w, "Subdomain: %s\n", tunnel.Subdomain)
	fmt.Fprintf(w, "Public URL: %s\n", tunnel.PublicURL)
	fmt.Fprintf(w, "Protocol: %s\n", defaultString(tunnel.Protocol, "http"))
	fmt.Fprintf(w, "Agent version: %s\n", defaultString(tunnel.AgentVersion, "-"))
	fmt.Fprintf(w, "Protocol version: %s\n", defaultString(tunnel.ProtocolVersion, "-"))
	fmt.Fprintf(w, "Connected: %s\n", formatTunnelTime(tunnel.ConnectedAt))
	fmt.Fprintf(w, "Uptime: %s\n", formatTunnelDuration(time.Duration(tunnel.ConnectedSeconds)*time.Second))
	fmt.Fprintf(w, "Streams: %d/%d\n", tunnel.ActiveStreams, tunnel.StreamCapacity)
	fmt.Fprintf(w, "Recent requests: %d\n", recent.Count)
	fmt.Fprintf(w, "Recent errors: %d\n", recent.ErrorCount)
	if recent.LastRequestAt != nil {
		fmt.Fprintf(w, "Last request: %s status=%d outcome=%s id=%s\n", recent.LastRequestAt.Format(time.RFC3339), recent.LastStatus, defaultString(recent.LastOutcome, "-"), defaultString(recent.LastRequestID, "-"))
	} else {
		fmt.Fprintln(w, "Last request: -")
	}
	fmt.Fprintf(w, "Custom domains: %s\n", defaultString(strings.Join(recent.CustomDomains, ","), "-"))
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func formatTunnelTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format(time.RFC3339)
}

func formatTunnelDuration(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	return value.Round(time.Second).String()
}

func tunnelsUsageError() error {
	return fmt.Errorf("%s", tunnelsUsageText)
}

func printTunnelsUsage(w io.Writer) {
	fmt.Fprintln(w, tunnelsUsageText)
}

func printTunnelListHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook tunnels list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] [--json]

List active tunnels through the authenticated control-plane operator API.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Admin token with runtime_diagnostics scope. Defaults to PORTHOOK_CONTROL_ADMIN_TOKEN.
  --admin-token-stdin       Read the admin token from stdin.
  --timeout DURATION        HTTP timeout. Default: 10s.
  --json                    Write JSON output.`)
}

func printTunnelShowHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook tunnels show --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] TUNNEL_ID [--json]

Show one active tunnel through the authenticated control-plane operator API.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Admin token with runtime_diagnostics scope. Defaults to PORTHOOK_CONTROL_ADMIN_TOKEN.
  --admin-token-stdin       Read the admin token from stdin.
  --timeout DURATION        HTTP timeout. Default: 10s.
  --json                    Write JSON output.`)
}
