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
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const defaultHistoryTimeout = 10 * time.Second
const defaultHistoryLimit = 100

const historyUsageText = `usage: porthook history events --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [filters] [--json]
       porthook history requests --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [filters] [--json]
       porthook history help`

type historyEventConfig struct {
	tokenAdminConfig
	timeout   time.Duration
	event     string
	level     string
	requestID string
	remoteIP  string
	field     string
	since     string
	until     string
	cursor    string
	limit     int
}

type historyRequestConfig struct {
	tokenAdminConfig
	timeout   time.Duration
	subdomain string
	method    string
	host      string
	path      string
	status    int
	outcome   string
	requestID string
	tunnelID  string
	since     string
	until     string
	cursor    string
	limit     int
}

type historyAuditEventsResponse struct {
	Events     []historyAuditEvent `json:"events"`
	NextCursor string              `json:"next_cursor,omitempty"`
	Filters    map[string]any      `json:"filters,omitempty"`
}

type historyAuditEvent struct {
	Time      time.Time         `json:"time"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Event     string            `json:"event"`
	Method    string            `json:"method,omitempty"`
	Path      string            `json:"path,omitempty"`
	RemoteIP  string            `json:"remote_ip,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type historyRequestLogsResponse struct {
	RequestLogs []historyRequestLog `json:"request_logs"`
	NextCursor  string              `json:"next_cursor,omitempty"`
	Filters     map[string]any      `json:"filters,omitempty"`
}

type historyRequestLog struct {
	Time          time.Time `json:"time"`
	Method        string    `json:"method"`
	Host          string    `json:"host"`
	Path          string    `json:"path"`
	QueryPresent  bool      `json:"query_present"`
	RemoteIP      string    `json:"remote_ip"`
	RequestID     string    `json:"request_id,omitempty"`
	Subdomain     string    `json:"subdomain,omitempty"`
	CustomDomain  string    `json:"custom_domain,omitempty"`
	TunnelID      string    `json:"tunnel_id,omitempty"`
	StreamID      string    `json:"stream_id,omitempty"`
	Status        int       `json:"status"`
	Outcome       string    `json:"outcome"`
	RequestBytes  int64     `json:"request_bytes"`
	ResponseBytes int64     `json:"response_bytes"`
	DurationMS    int64     `json:"duration_ms"`
	Error         string    `json:"error,omitempty"`
}

type historyGatewayClient struct {
	baseURL    string
	adminToken string
	client     *http.Client
}

func runHistoryCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return historyUsageError()
	}

	switch args[0] {
	case "events":
		if wantsHelp(args[1:]) {
			printHistoryEventsHelp(stdout)
			return nil
		}
		return runHistoryEvents(args[1:], stdin, stdout, stderr)
	case "requests":
		if wantsHelp(args[1:]) {
			printHistoryRequestsHelp(stdout)
			return nil
		}
		return runHistoryRequests(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		printHistoryUsage(stdout)
		return nil
	default:
		return historyUsageError()
	}
}

func runHistoryEvents(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseHistoryEventConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newTokenAdminClient(cfg.tokenAdminConfig)
	client.httpClient.Timeout = cfg.timeout
	listed, err := client.listHistoryEvents(context.Background(), cfg)
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printHistoryAuditEvents(stdout, listed)
	return nil
}

func runHistoryRequests(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseHistoryRequestConfig(args, stdin, stderr)
	if err != nil {
		return err
	}
	client := newHistoryGatewayClient(cfg)
	listed, err := client.listHistoryRequests(context.Background(), cfg)
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printHistoryRequestLogs(stdout, listed)
	return nil
}

func parseHistoryEventConfig(args []string, stdin io.Reader, stderr io.Writer) (historyEventConfig, error) {
	var cfg historyEventConfig
	cfg.timeout = defaultHistoryTimeout
	cfg.limit = defaultHistoryLimit
	fs := newTokenAdminFlagSet("history events", &cfg.tokenAdminConfig, stderr)
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "HTTP timeout")
	fs.IntVar(&cfg.limit, "limit", cfg.limit, "maximum events to return")
	fs.StringVar(&cfg.event, "event", cfg.event, "event name filter")
	fs.StringVar(&cfg.level, "level", cfg.level, "level filter")
	fs.StringVar(&cfg.requestID, "request-id", cfg.requestID, "request ID filter")
	fs.StringVar(&cfg.remoteIP, "remote-ip", cfg.remoteIP, "remote IP filter")
	fs.StringVar(&cfg.field, "field", cfg.field, "audit field text filter")
	fs.StringVar(&cfg.since, "since", cfg.since, "RFC3339 lower time bound")
	fs.StringVar(&cfg.until, "until", cfg.until, "RFC3339 upper time bound")
	fs.StringVar(&cfg.cursor, "cursor", cfg.cursor, "pagination cursor")
	if err := fs.Parse(args); err != nil {
		return historyEventConfig{}, err
	}
	if fs.NArg() > 0 {
		return historyEventConfig{}, fmt.Errorf("usage: porthook history events --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [filters] [--json]")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return historyEventConfig{}, err
	}
	if err := finalizeHistoryEventConfig(&cfg); err != nil {
		return historyEventConfig{}, err
	}
	return cfg, nil
}

func finalizeHistoryEventConfig(cfg *historyEventConfig) error {
	if cfg.timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	cfg.limit = normalizeHistoryLimit(cfg.limit)
	cfg.event = strings.TrimSpace(cfg.event)
	cfg.level = strings.ToUpper(strings.TrimSpace(cfg.level))
	cfg.requestID = strings.TrimSpace(cfg.requestID)
	cfg.remoteIP = strings.TrimSpace(cfg.remoteIP)
	cfg.field = strings.TrimSpace(cfg.field)
	cfg.cursor = strings.TrimSpace(cfg.cursor)
	return validateHistoryWindow(cfg.since, cfg.until)
}

func parseHistoryRequestConfig(args []string, stdin io.Reader, stderr io.Writer) (historyRequestConfig, error) {
	cfg := defaultHistoryRequestConfig()
	fs := newTokenAdminFlagSet("history requests", &cfg.tokenAdminConfig, stderr)
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "HTTP timeout")
	fs.IntVar(&cfg.limit, "limit", cfg.limit, "maximum request logs to return")
	fs.StringVar(&cfg.subdomain, "subdomain", cfg.subdomain, "subdomain filter")
	fs.StringVar(&cfg.method, "method", cfg.method, "HTTP method filter")
	fs.StringVar(&cfg.host, "host", cfg.host, "host filter")
	fs.StringVar(&cfg.path, "path", cfg.path, "path filter")
	fs.IntVar(&cfg.status, "status", cfg.status, "HTTP status filter")
	fs.StringVar(&cfg.outcome, "outcome", cfg.outcome, "gateway outcome filter")
	fs.StringVar(&cfg.requestID, "request-id", cfg.requestID, "request ID filter")
	fs.StringVar(&cfg.tunnelID, "tunnel-id", cfg.tunnelID, "tunnel ID filter")
	fs.StringVar(&cfg.since, "since", cfg.since, "RFC3339 lower time bound")
	fs.StringVar(&cfg.until, "until", cfg.until, "RFC3339 upper time bound")
	fs.StringVar(&cfg.cursor, "cursor", cfg.cursor, "pagination cursor")
	if err := fs.Parse(args); err != nil {
		return historyRequestConfig{}, err
	}
	if fs.NArg() > 0 {
		return historyRequestConfig{}, fmt.Errorf("usage: porthook history requests --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [filters] [--json]")
	}
	if err := finalizeHistoryRequestConfig(fs, &cfg, stdin, stderr); err != nil {
		return historyRequestConfig{}, err
	}
	return cfg, nil
}

func defaultHistoryRequestConfig() historyRequestConfig {
	return historyRequestConfig{
		timeout: defaultHistoryTimeout,
		limit:   defaultHistoryLimit,
	}
}

func finalizeHistoryRequestConfig(fs *flag.FlagSet, cfg *historyRequestConfig, stdin io.Reader, stderr io.Writer) error {
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return err
	}
	if cfg.timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	cfg.limit = normalizeHistoryLimit(cfg.limit)
	cfg.subdomain = strings.TrimSpace(cfg.subdomain)
	cfg.method = strings.ToUpper(strings.TrimSpace(cfg.method))
	cfg.host = strings.TrimSpace(cfg.host)
	cfg.path = strings.TrimSpace(cfg.path)
	cfg.outcome = strings.TrimSpace(cfg.outcome)
	cfg.requestID = strings.TrimSpace(cfg.requestID)
	cfg.tunnelID = strings.TrimSpace(cfg.tunnelID)
	cfg.cursor = strings.TrimSpace(cfg.cursor)
	if cfg.status != 0 && (cfg.status < 100 || cfg.status > 599) {
		return fmt.Errorf("status must be between 100 and 599")
	}
	return validateHistoryWindow(cfg.since, cfg.until)
}

func validateHistoryWindow(since string, until string) error {
	sinceTime, err := parseOptionalHistoryTime(since, "since")
	if err != nil {
		return err
	}
	untilTime, err := parseOptionalHistoryTime(until, "until")
	if err != nil {
		return err
	}
	if sinceTime != nil && untilTime != nil && !sinceTime.Before(*untilTime) {
		return fmt.Errorf("since must be before until")
	}
	return nil
}

func normalizeHistoryLimit(value int) int {
	if value <= 0 {
		return defaultHistoryLimit
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func (c tokenAdminClient) listHistoryEvents(ctx context.Context, cfg historyEventConfig) (historyAuditEventsResponse, error) {
	var listed historyAuditEventsResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/events?"+historyEventQuery(cfg), nil, &listed); err != nil {
		return historyAuditEventsResponse{}, err
	}
	return listed, nil
}

func newHistoryGatewayClient(cfg historyRequestConfig) historyGatewayClient {
	return historyGatewayClient{
		baseURL:    cfg.controlPlaneURL,
		adminToken: cfg.adminToken,
		client:     &http.Client{Timeout: cfg.timeout},
	}
}

func (c historyGatewayClient) listHistoryRequests(ctx context.Context, cfg historyRequestConfig) (historyRequestLogsResponse, error) {
	var listed historyRequestLogsResponse
	if err := c.getJSON(ctx, "/api/v1/gateway/request-logs?"+historyRequestQuery(cfg), &listed); err != nil {
		return historyRequestLogsResponse{}, err
	}
	return listed, nil
}

func (c historyGatewayClient) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build gateway request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	req.Header.Set("User-Agent", "porthook/"+version+" history")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
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

func historyEventQuery(cfg historyEventConfig) string {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(cfg.limit))
	appendHistoryFilter(query, "event", cfg.event)
	appendHistoryFilter(query, "level", cfg.level)
	appendHistoryFilter(query, "request_id", cfg.requestID)
	appendHistoryFilter(query, "remote_ip", cfg.remoteIP)
	appendHistoryFilter(query, "field", cfg.field)
	appendHistoryTimeFilter(query, "since", cfg.since)
	appendHistoryTimeFilter(query, "until", cfg.until)
	appendHistoryFilter(query, "cursor", cfg.cursor)
	return query.Encode()
}

func historyRequestQuery(cfg historyRequestConfig) string {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(cfg.limit))
	appendHistoryFilter(query, "subdomain", cfg.subdomain)
	appendHistoryFilter(query, "method", cfg.method)
	appendHistoryFilter(query, "host", cfg.host)
	appendHistoryFilter(query, "path", cfg.path)
	if cfg.status != 0 {
		query.Set("status", strconv.Itoa(cfg.status))
	}
	appendHistoryFilter(query, "outcome", cfg.outcome)
	appendHistoryFilter(query, "request_id", cfg.requestID)
	appendHistoryFilter(query, "tunnel_id", cfg.tunnelID)
	appendHistoryTimeFilter(query, "since", cfg.since)
	appendHistoryTimeFilter(query, "until", cfg.until)
	appendHistoryFilter(query, "cursor", cfg.cursor)
	return query.Encode()
}

func appendHistoryFilter(query url.Values, name string, value string) {
	if value != "" {
		query.Set(name, value)
	}
}

func appendHistoryTimeFilter(query url.Values, name string, value string) {
	parsed, _ := parseOptionalHistoryTime(value, name)
	if parsed != nil {
		query.Set(name, parsed.Format(time.RFC3339Nano))
	}
}

func parseOptionalHistoryTime(value string, name string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be an RFC3339 timestamp", name)
	}
	return &parsed, nil
}

func printHistoryAuditEvents(w io.Writer, listed historyAuditEventsResponse) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tLEVEL\tEVENT\tREQUEST ID\tREMOTE IP\tMESSAGE\tFIELDS")
	for _, event := range listed.Events {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			formatHistoryTime(event.Time),
			defaultString(event.Level, "-"),
			defaultString(event.Event, "-"),
			defaultString(event.RequestID, "-"),
			defaultString(event.RemoteIP, "-"),
			defaultString(event.Message, "-"),
			historyFieldsText(event.Fields),
		)
	}
	_ = tw.Flush()
	printHistoryNextCursor(w, listed.NextCursor)
}

func printHistoryRequestLogs(w io.Writer, listed historyRequestLogsResponse) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tSUBDOMAIN\tHOST\tMETHOD\tSTATUS\tOUTCOME\tPATH\tREQUEST ID\tDURATION")
	for _, entry := range listed.RequestLogs {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			formatHistoryTime(entry.Time),
			defaultString(entry.Subdomain, "-"),
			defaultString(entry.Host, "-"),
			defaultString(entry.Method, "-"),
			historyStatusText(entry.Status),
			defaultString(entry.Outcome, "-"),
			defaultString(entry.Path, "-"),
			defaultString(entry.RequestID, "-"),
			formatTunnelDuration(time.Duration(entry.DurationMS)*time.Millisecond),
		)
	}
	_ = tw.Flush()
	printHistoryNextCursor(w, listed.NextCursor)
}

func historyFieldsText(fields map[string]string) string {
	if len(fields) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fields[key])
	}
	return strings.Join(parts, ",")
}

func historyStatusText(status int) string {
	if status == 0 {
		return "-"
	}
	return strconv.Itoa(status)
}

func formatHistoryTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format(time.RFC3339)
}

func printHistoryNextCursor(w io.Writer, cursor string) {
	if cursor != "" {
		fmt.Fprintf(w, "Next cursor: %s\n", cursor)
	}
}

func historyUsageError() error {
	return fmt.Errorf("%s", historyUsageText)
}

func printHistoryUsage(w io.Writer) {
	fmt.Fprintln(w, historyUsageText)
}

func printHistoryEventsHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook history events --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [filters] [--json]

List control-plane audit events.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --timeout DURATION        HTTP timeout. Default: 10s.
  --limit N                 Maximum events to return. Default: 100. Maximum: 1000.
  --event VALUE             Event name filter.
  --level LEVEL             Level filter.
  --request-id VALUE        Request ID filter.
  --remote-ip VALUE         Remote IP filter.
  --field VALUE             Audit field text filter.
  --since TIMESTAMP         RFC3339 lower time bound.
  --until TIMESTAMP         RFC3339 upper time bound.
  --cursor CURSOR           Pagination cursor from the previous response.
  --json                    Write JSON output.`)
}

func printHistoryRequestsHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook history requests --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [filters] [--json]

List gateway request logs through the authenticated control-plane operator API.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Admin token with audit_history scope. Defaults to PORTHOOK_CONTROL_ADMIN_TOKEN.
  --admin-token-stdin       Read the admin token from stdin.
  --timeout DURATION        HTTP timeout. Default: 10s.
  --limit N                 Maximum request logs to return. Default: 100. Maximum: 1000.
  --subdomain VALUE         Subdomain filter.
  --method METHOD           HTTP method filter.
  --host VALUE              Host filter.
  --path VALUE              Path filter.
  --status STATUS           HTTP status filter.
  --outcome VALUE           Gateway outcome filter.
  --request-id VALUE        Request ID filter.
  --tunnel-id VALUE         Tunnel ID filter.
  --since TIMESTAMP         RFC3339 lower time bound.
  --until TIMESTAMP         RFC3339 upper time bound.
  --cursor CURSOR           Pagination cursor from the previous response.
  --json                    Write JSON output.`)
}
