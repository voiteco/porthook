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
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultOperationalExportTimeout = 10 * time.Second
const defaultOperationalExportLimit = 100
const operationalExportSchemaVersion = 2

const operationalExportUsageText = `usage: porthook export [--gateway URL] [--control-plane URL] [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] [--event-limit N] [--request-log-limit N] [--output FILE]`

type operationalExportConfig struct {
	gatewayURL      string
	controlPlaneURL string
	adminToken      string
	adminTokenStdin bool
	timeout         time.Duration
	eventLimit      int
	requestLogLimit int
	outputPath      string
}

type operationalExportSnapshot struct {
	SchemaVersion int                            `json:"schema_version"`
	ExportedAt    time.Time                      `json:"exported_at"`
	Version       string                         `json:"version"`
	Sources       operationalExportSources       `json:"sources"`
	Diagnostics   *doctorReport                  `json:"diagnostics,omitempty"`
	ControlPlane  *operationalExportControlPlane `json:"control_plane,omitempty"`
	Gateway       *operationalExportGateway      `json:"gateway,omitempty"`
	Errors        []operationalExportError       `json:"errors,omitempty"`
}

type operationalExportSources struct {
	ControlPlaneURL string `json:"control_plane_url,omitempty"`
	GatewayURL      string `json:"gateway_url,omitempty"`
	EventLimit      int    `json:"event_limit"`
	RequestLogLimit int    `json:"request_log_limit"`
}

type operationalExportControlPlane struct {
	Status             *doctorControlPlaneStatus `json:"status,omitempty"`
	Tokens             []tokenSummary            `json:"tokens"`
	ReservedSubdomains []reservationSummary      `json:"reserved_subdomains"`
	CustomDomains      []customDomainSummary     `json:"custom_domains"`
	AccessPolicies     []accessPolicySummary     `json:"access_policies"`
	AuditEvents        []json.RawMessage         `json:"audit_events"`
	AuditEventFilters  map[string]any            `json:"audit_event_filters,omitempty"`
	AuditEventCursor   string                    `json:"audit_event_next_cursor,omitempty"`
}

type operationalExportGateway struct {
	Tunnels           []tunnelSummaryCLI            `json:"tunnels"`
	TunnelDetails     []tunnelDetailCLI             `json:"tunnel_details"`
	Runtime           map[string]any                `json:"runtime,omitempty"`
	Metrics           []operationalExportMetric     `json:"metrics"`
	MetricsText       string                        `json:"metrics_text,omitempty"`
	RequestLogs       []operationalExportRequestLog `json:"request_logs"`
	RequestLogFilters map[string]any                `json:"request_log_filters,omitempty"`
	RequestLogCursor  string                        `json:"request_log_next_cursor,omitempty"`
}

type operationalExportMetric struct {
	Name  string `json:"name"`
	Type  string `json:"type,omitempty"`
	Value string `json:"value"`
	Help  string `json:"help,omitempty"`
}

type operationalExportRequestLogsResponse struct {
	RequestLogs []operationalExportRequestLog `json:"request_logs"`
	NextCursor  string                        `json:"next_cursor,omitempty"`
	Filters     map[string]any                `json:"filters,omitempty"`
}

type operationalExportAuditEventsResponse struct {
	Events     []json.RawMessage `json:"events"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Filters    map[string]any    `json:"filters,omitempty"`
}

type operationalExportRequestLog struct {
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

type operationalExportError struct {
	Component string `json:"component"`
	Endpoint  string `json:"endpoint"`
	Error     string `json:"error"`
}

func runOperationalExportCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if wantsHelp(args) {
		printOperationalExportHelp(stdout)
		return nil
	}
	cfg, err := parseOperationalExportConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	snapshot := collectOperationalExport(context.Background(), cfg)
	data, err := marshalPrettyJSON(snapshot)
	if err != nil {
		return err
	}
	if cfg.outputPath != "" {
		if err := os.WriteFile(cfg.outputPath, data, 0o600); err != nil {
			return fmt.Errorf("write export file: %w", err)
		}
		fmt.Fprintf(stdout, "Wrote operational export: %s\n", cfg.outputPath)
		if len(snapshot.Errors) > 0 {
			fmt.Fprintf(stderr, "Operational export completed with %d error%s.\n", len(snapshot.Errors), pluralSuffix(len(snapshot.Errors)))
		}
		return nil
	}
	_, err = stdout.Write(data)
	return err
}

func parseOperationalExportConfig(args []string, stdin io.Reader, stderr io.Writer) (operationalExportConfig, error) {
	var cfg operationalExportConfig
	fs := newOperationalExportFlagSet(&cfg, stderr)
	if err := fs.Parse(args); err != nil {
		return operationalExportConfig{}, err
	}
	if fs.NArg() > 0 {
		return operationalExportConfig{}, fmt.Errorf("%s", operationalExportUsageText)
	}
	if err := finalizeOperationalExportConfig(fs, &cfg, stdin); err != nil {
		return operationalExportConfig{}, err
	}
	return cfg, nil
}

func newOperationalExportFlagSet(cfg *operationalExportConfig, stderr io.Writer) *flag.FlagSet {
	cfg.gatewayURL = strings.TrimSpace(os.Getenv("PORTHOOK_GATEWAY_URL"))
	if cfg.gatewayURL == "" {
		cfg.gatewayURL = defaultDoctorGatewayURL
	}
	cfg.controlPlaneURL = strings.TrimSpace(os.Getenv("PORTHOOK_CONTROL_PLANE_URL"))
	cfg.adminToken = strings.TrimSpace(os.Getenv("PORTHOOK_CONTROL_ADMIN_TOKEN"))
	cfg.timeout = defaultOperationalExportTimeout
	cfg.eventLimit = defaultOperationalExportLimit
	cfg.requestLogLimit = defaultOperationalExportLimit

	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.gatewayURL, "gateway", cfg.gatewayURL, "gateway public URL")
	fs.StringVar(&cfg.controlPlaneURL, "control-plane", cfg.controlPlaneURL, "control-plane API URL")
	fs.StringVar(&cfg.adminToken, "admin-token", cfg.adminToken, "control-plane admin token")
	fs.BoolVar(&cfg.adminTokenStdin, "admin-token-stdin", cfg.adminTokenStdin, "read control-plane admin token from stdin")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "HTTP timeout")
	fs.IntVar(&cfg.eventLimit, "event-limit", cfg.eventLimit, "control-plane audit event limit")
	fs.IntVar(&cfg.requestLogLimit, "request-log-limit", cfg.requestLogLimit, "gateway request log limit")
	fs.StringVar(&cfg.outputPath, "output", cfg.outputPath, "write JSON export to file instead of stdout")
	return fs
}

func finalizeOperationalExportConfig(fs *flag.FlagSet, cfg *operationalExportConfig, stdin io.Reader) error {
	cfg.gatewayURL = strings.TrimSpace(cfg.gatewayURL)
	if cfg.gatewayURL != "" {
		gatewayURL, err := normalizeDoctorURL(cfg.gatewayURL, "gateway URL")
		if err != nil {
			return err
		}
		cfg.gatewayURL = gatewayURL
	}

	cfg.controlPlaneURL = strings.TrimSpace(cfg.controlPlaneURL)
	if cfg.controlPlaneURL != "" {
		controlPlaneURL, err := normalizeControlPlaneURL(cfg.controlPlaneURL)
		if err != nil {
			return err
		}
		cfg.controlPlaneURL = controlPlaneURL
	}

	cfg.adminToken = strings.TrimSpace(cfg.adminToken)
	if cfg.adminToken != "" && cfg.adminTokenStdin {
		return fmt.Errorf("--admin-token and --admin-token-stdin are mutually exclusive")
	}
	if cfg.adminTokenStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("read admin token from stdin: %w", err)
		}
		cfg.adminToken = strings.TrimSpace(string(data))
		if cfg.adminToken == "" {
			return fmt.Errorf("admin token from stdin is empty")
		}
	}

	if cfg.gatewayURL == "" && cfg.controlPlaneURL == "" {
		return fmt.Errorf("gateway URL or control-plane URL is required")
	}
	if cfg.timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	cfg.eventLimit = normalizeOperationalExportLimit(cfg.eventLimit)
	cfg.requestLogLimit = normalizeOperationalExportLimit(cfg.requestLogLimit)
	cfg.outputPath = strings.TrimSpace(cfg.outputPath)
	return nil
}

func normalizeOperationalExportLimit(value int) int {
	if value <= 0 {
		return defaultOperationalExportLimit
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func collectOperationalExport(ctx context.Context, cfg operationalExportConfig) operationalExportSnapshot {
	snapshot := operationalExportSnapshot{
		SchemaVersion: operationalExportSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		Version:       version,
		Sources: operationalExportSources{
			ControlPlaneURL: cfg.controlPlaneURL,
			GatewayURL:      cfg.gatewayURL,
			EventLimit:      cfg.eventLimit,
			RequestLogLimit: cfg.requestLogLimit,
		},
	}
	diagnostics := collectDoctorReport(ctx, doctorConfig{
		gatewayURL:      cfg.gatewayURL,
		controlPlaneURL: cfg.controlPlaneURL,
		adminToken:      cfg.adminToken,
		timeout:         cfg.timeout,
	})
	snapshot.Diagnostics = &diagnostics
	if cfg.controlPlaneURL != "" {
		collectControlPlaneExport(ctx, cfg, &snapshot)
	}
	if cfg.gatewayURL != "" {
		collectGatewayExport(ctx, cfg, &snapshot)
	}
	return snapshot
}

func collectControlPlaneExport(ctx context.Context, cfg operationalExportConfig, snapshot *operationalExportSnapshot) {
	controlPlane := &operationalExportControlPlane{
		Tokens:             []tokenSummary{},
		ReservedSubdomains: []reservationSummary{},
		CustomDomains:      []customDomainSummary{},
		AccessPolicies:     []accessPolicySummary{},
		AuditEvents:        []json.RawMessage{},
	}
	snapshot.ControlPlane = controlPlane

	httpClient := &http.Client{Timeout: cfg.timeout}
	var status doctorControlPlaneStatus
	if err := exportGETJSON(ctx, httpClient, cfg.controlPlaneURL+"/api/v1/status", "", &status); err != nil {
		addOperationalExportError(snapshot, "control-plane", "/api/v1/status", err)
	} else {
		controlPlane.Status = &status
	}

	adminCfg := tokenAdminConfig{
		controlPlaneURL: cfg.controlPlaneURL,
		adminToken:      cfg.adminToken,
	}
	adminClient := newTokenAdminClient(adminCfg)
	adminClient.httpClient.Timeout = cfg.timeout

	if listed, err := adminClient.listTokens(ctx); err != nil {
		addOperationalExportError(snapshot, "control-plane", "/api/v1/tokens", err)
	} else {
		controlPlane.Tokens = listed.Tokens
	}
	if listed, err := adminClient.listReservations(ctx); err != nil {
		addOperationalExportError(snapshot, "control-plane", "/api/v1/reserved-subdomains", err)
	} else {
		controlPlane.ReservedSubdomains = listed.ReservedSubdomains
	}
	if listed, err := adminClient.listCustomDomains(ctx); err != nil {
		addOperationalExportError(snapshot, "control-plane", "/api/v1/custom-domains", err)
	} else {
		controlPlane.CustomDomains = listed.CustomDomains
	}
	if listed, err := adminClient.listAccessPolicies(ctx); err != nil {
		addOperationalExportError(snapshot, "control-plane", "/api/v1/access-policies", err)
	} else {
		controlPlane.AccessPolicies = listed.AccessPolicies
	}
	if listed, err := adminClient.listAuditEvents(ctx, cfg.eventLimit); err != nil {
		addOperationalExportError(snapshot, "control-plane", "/api/v1/events", err)
	} else {
		controlPlane.AuditEvents = listed.Events
		controlPlane.AuditEventFilters = listed.Filters
		controlPlane.AuditEventCursor = listed.NextCursor
	}
}

func collectGatewayExport(ctx context.Context, cfg operationalExportConfig, snapshot *operationalExportSnapshot) {
	gateway := &operationalExportGateway{
		Tunnels:       []tunnelSummaryCLI{},
		TunnelDetails: []tunnelDetailCLI{},
		Metrics:       []operationalExportMetric{},
		RequestLogs:   []operationalExportRequestLog{},
	}
	snapshot.Gateway = gateway

	gatewayClient := newTunnelAPIClient(tunnelCLIConfig{
		gatewayURL: cfg.gatewayURL,
		timeout:    cfg.timeout,
	})

	if listed, err := gatewayClient.listTunnels(ctx); err != nil {
		addOperationalExportError(snapshot, "gateway", "/api/v1/tunnels", err)
	} else {
		gateway.Tunnels = listed.Tunnels
		for _, tunnel := range listed.Tunnels {
			shown, err := gatewayClient.showTunnel(ctx, tunnel.TunnelID)
			if err != nil {
				addOperationalExportError(snapshot, "gateway", "/api/v1/tunnels/"+tunnel.TunnelID, err)
				continue
			}
			gateway.TunnelDetails = append(gateway.TunnelDetails, shown.Tunnel)
		}
	}

	var runtime struct {
		Runtime map[string]any `json:"runtime"`
	}
	if err := gatewayClient.do(ctx, http.MethodGet, "/api/v1/runtime", &runtime); err != nil {
		addOperationalExportError(snapshot, "gateway", "/api/v1/runtime", err)
	} else {
		gateway.Runtime = runtime.Runtime
	}

	if metricsText, err := gatewayClient.getText(ctx, "/metrics"); err != nil {
		addOperationalExportError(snapshot, "gateway", "/metrics", err)
	} else {
		gateway.MetricsText = metricsText
		gateway.Metrics = parseOperationalExportMetrics(metricsText)
	}

	var logs operationalExportRequestLogsResponse
	requestLogsPath := "/api/v1/request-logs?limit=" + strconv.Itoa(cfg.requestLogLimit)
	if err := gatewayClient.do(ctx, http.MethodGet, requestLogsPath, &logs); err != nil {
		addOperationalExportError(snapshot, "gateway", "/api/v1/request-logs", err)
	} else {
		gateway.RequestLogs = logs.RequestLogs
		gateway.RequestLogFilters = logs.Filters
		gateway.RequestLogCursor = logs.NextCursor
	}
}

func (c tokenAdminClient) listAuditEvents(ctx context.Context, limit int) (operationalExportAuditEventsResponse, error) {
	var listed operationalExportAuditEventsResponse
	path := "/api/v1/events?limit=" + strconv.Itoa(limit)
	if err := c.do(ctx, http.MethodGet, path, nil, &listed); err != nil {
		return operationalExportAuditEventsResponse{}, err
	}
	return listed, nil
}

func (c tunnelAPIClient) getText(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/plain, */*;q=0.1")
	req.Header.Set("User-Agent", "porthook/"+version+" export")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", gatewayAPIStatusError(resp.StatusCode, readLimitedString(resp.Body, 4096))
	}
	return readLimitedString(resp.Body, 8*1024*1024), nil
}

func exportGETJSON(ctx context.Context, client *http.Client, rawURL string, bearerToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "porthook/"+version+" export")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(readLimitedString(resp.Body, 4096))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("endpoint returned %d: %s", resp.StatusCode, message)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func parseOperationalExportMetrics(text string) []operationalExportMetric {
	metadata := map[string]operationalExportMetric{}
	var metrics []operationalExportMetric
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# HELP ") {
			rest := strings.TrimPrefix(trimmed, "# HELP ")
			name, help, ok := strings.Cut(rest, " ")
			if ok && name != "" {
				meta := metadata[name]
				meta.Help = help
				metadata[name] = meta
			}
			continue
		}
		if strings.HasPrefix(trimmed, "# TYPE ") {
			fields := strings.Fields(strings.TrimPrefix(trimmed, "# TYPE "))
			if len(fields) >= 2 {
				meta := metadata[fields[0]]
				meta.Type = fields[1]
				metadata[fields[0]] = meta
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		baseName, _, _ := strings.Cut(name, "{")
		meta := metadata[baseName]
		metrics = append(metrics, operationalExportMetric{
			Name:  name,
			Type:  meta.Type,
			Value: fields[1],
			Help:  meta.Help,
		})
	}
	return metrics
}

func addOperationalExportError(snapshot *operationalExportSnapshot, component string, endpoint string, err error) {
	snapshot.Errors = append(snapshot.Errors, operationalExportError{
		Component: component,
		Endpoint:  endpoint,
		Error:     err.Error(),
	})
}

func marshalPrettyJSON(payload any) ([]byte, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode export: %w", err)
	}
	return append(data, '\n'), nil
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func printOperationalExportHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook export [--gateway URL] [--control-plane URL] [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] [--event-limit N] [--request-log-limit N] [--output FILE]

Write a best-effort operational JSON snapshot. The export includes public gateway state, safe control-plane summaries, diagnostics, audit events, metrics, and request logs. It does not include plaintext agent tokens, policy secrets, or local target URLs.

Options:
  --gateway URL             Gateway public URL. Defaults to PORTHOOK_GATEWAY_URL or http://localhost:8080.
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Control-plane admin token for protected control-plane sections.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --timeout DURATION        HTTP timeout. Default: 10s.
  --event-limit N           Audit event limit. Default: 100. Maximum: 1000.
  --request-log-limit N     Gateway request log limit. Default: 100. Maximum: 1000.
  --output FILE             Write JSON export to file instead of stdout.`)
}
