// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
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

const (
	defaultDoctorGatewayURL = "http://localhost:8080"
	defaultDoctorTimeout    = 5 * time.Second
)

const doctorUsageText = `usage: porthook doctor --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] [--json]`

type doctorConfig struct {
	gatewayURL       string
	controlPlaneURL  string
	adminToken       string
	adminTokenStdin  bool
	adminTokenIsFlag bool
	timeout          time.Duration
	jsonOutput       bool
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

type doctorCheck struct {
	Name       string `json:"name"`
	Component  string `json:"component"`
	Method     string `json:"method,omitempty"`
	URL        string `json:"url,omitempty"`
	OK         bool   `json:"ok"`
	Skipped    bool   `json:"skipped,omitempty"`
	Status     int    `json:"status,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

type doctorControlPlaneStatus struct {
	Status  string `json:"status"`
	Ready   bool   `json:"ready"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

type doctorTunnelList struct {
	Tunnels []json.RawMessage `json:"tunnels"`
}

type doctorAuditEventList struct {
	Events []json.RawMessage `json:"events"`
}

type doctorRuntimeResponse struct {
	Runtime struct {
		UptimeSeconds      int64 `json:"uptime_seconds"`
		ActiveTunnels      int   `json:"active_tunnels"`
		ActiveStreams      int   `json:"active_streams"`
		RequestLogEntries  int   `json:"request_log_entries"`
		RequestLogCapacity int   `json:"request_log_capacity"`
	} `json:"runtime"`
}

type doctorRequestLogList struct {
	RequestLogs []json.RawMessage `json:"request_logs"`
}

func runDoctorCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseDoctorConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	report := collectDoctorReport(context.Background(), cfg)

	if cfg.jsonOutput {
		if err := writeJSONOutput(stdout, report); err != nil {
			return err
		}
	} else {
		printDoctorReport(stdout, report)
	}

	if !report.OK {
		return fmt.Errorf("doctor found %d failed check(s)", report.failedCount())
	}
	return nil
}

func collectDoctorReport(ctx context.Context, cfg doctorConfig) doctorReport {
	client := &http.Client{Timeout: cfg.timeout}
	report := doctorReport{OK: true}

	if cfg.gatewayURL != "" {
		report.add(runDoctorGET(ctx, client, "gateway", "health", cfg.gatewayURL+"/healthz", nil, plainDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "readiness", cfg.gatewayURL+"/readyz", nil, plainDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "tunnels API", cfg.gatewayURL+"/api/v1/tunnels", nil, tunnelsDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "runtime API", cfg.gatewayURL+"/api/v1/runtime", nil, runtimeDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "request logs API", cfg.gatewayURL+"/api/v1/request-logs?limit=1", nil, requestLogsDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "metrics", cfg.gatewayURL+"/metrics", nil, metricsDoctorDetail))
	} else if cfg.controlPlaneURL == "" || cfg.adminToken == "" {
		report.add(skippedDoctorCheck("gateway", "health", "control-plane operator access is not configured"))
		report.add(skippedDoctorCheck("gateway", "readiness", "control-plane operator access is not configured"))
		report.add(skippedDoctorCheck("gateway", "tunnels API", "control-plane operator access is not configured"))
		report.add(skippedDoctorCheck("gateway", "runtime API", "control-plane operator access is not configured"))
		report.add(skippedDoctorCheck("gateway", "request logs API", "control-plane operator access is not configured"))
		report.add(skippedDoctorCheck("gateway", "metrics", "control-plane operator access is not configured"))
	} else {
		headers := map[string]string{"Authorization": "Bearer " + cfg.adminToken}
		report.add(runDoctorGET(ctx, client, "gateway", "health", cfg.controlPlaneURL+"/api/v1/gateway/healthz", headers, plainDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "readiness", cfg.controlPlaneURL+"/api/v1/gateway/readyz", headers, plainDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "tunnels API", cfg.controlPlaneURL+"/api/v1/gateway/tunnels", headers, tunnelsDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "runtime API", cfg.controlPlaneURL+"/api/v1/gateway/runtime", headers, runtimeDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "request logs API", cfg.controlPlaneURL+"/api/v1/gateway/request-logs?limit=1", headers, requestLogsDoctorDetail))
		report.add(runDoctorGET(ctx, client, "gateway", "metrics", cfg.controlPlaneURL+"/api/v1/gateway/metrics", headers, metricsDoctorDetail))
	}

	if cfg.controlPlaneURL == "" {
		report.add(skippedDoctorCheck("control-plane", "health", "control-plane URL is not configured"))
		report.add(skippedDoctorCheck("control-plane", "readiness", "control-plane URL is not configured"))
		report.add(skippedDoctorCheck("control-plane", "status API", "control-plane URL is not configured"))
	} else {
		report.add(runDoctorGET(ctx, client, "control-plane", "health", cfg.controlPlaneURL+"/healthz", nil, plainDoctorDetail))
		report.add(runDoctorGET(ctx, client, "control-plane", "readiness", cfg.controlPlaneURL+"/readyz", nil, plainDoctorDetail))
		report.add(runDoctorGET(ctx, client, "control-plane", "status API", cfg.controlPlaneURL+"/api/v1/status", nil, controlPlaneStatusDoctorDetail))
		if cfg.adminToken == "" {
			report.add(skippedDoctorCheck("control-plane", "audit events API", "admin token is not configured"))
		} else {
			headers := map[string]string{"Authorization": "Bearer " + cfg.adminToken}
			report.add(runDoctorGET(ctx, client, "control-plane", "audit events API", cfg.controlPlaneURL+"/api/v1/events?limit=1", headers, auditEventsDoctorDetail))
		}
	}

	return report
}

func parseDoctorConfig(args []string, stdin io.Reader, stderr io.Writer) (doctorConfig, error) {
	cfg := doctorConfig{
		controlPlaneURL: strings.TrimSpace(os.Getenv("PORTHOOK_CONTROL_PLANE_URL")),
		adminToken:      strings.TrimSpace(os.Getenv("PORTHOOK_CONTROL_ADMIN_TOKEN")),
		timeout:         defaultDoctorTimeout,
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.controlPlaneURL, "control-plane", cfg.controlPlaneURL, "control-plane API URL")
	fs.StringVar(&cfg.adminToken, "admin-token", cfg.adminToken, "control-plane admin token")
	fs.BoolVar(&cfg.adminTokenStdin, "admin-token-stdin", cfg.adminTokenStdin, "read control-plane admin token from stdin")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "per-check HTTP timeout")
	fs.BoolVar(&cfg.jsonOutput, "json", cfg.jsonOutput, "write JSON output")
	if err := fs.Parse(args); err != nil {
		return doctorConfig{}, err
	}
	if fs.NArg() > 0 {
		return doctorConfig{}, fmt.Errorf("%s", doctorUsageText)
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "admin-token" {
			cfg.adminTokenIsFlag = true
		}
	})

	cfg.controlPlaneURL = strings.TrimSpace(cfg.controlPlaneURL)
	controlPlaneURL, err := normalizeDoctorURL(cfg.controlPlaneURL, "control-plane URL")
	if err != nil {
		return doctorConfig{}, err
	}
	cfg.controlPlaneURL = controlPlaneURL
	if cfg.timeout <= 0 {
		return doctorConfig{}, fmt.Errorf("timeout must be positive")
	}

	if cfg.adminTokenStdin {
		adminToken := cfg.adminToken
		if !cfg.adminTokenIsFlag {
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
			return doctorConfig{}, err
		}
		cfg.adminToken = adminToken
	} else {
		cfg.adminToken = strings.TrimSpace(cfg.adminToken)
	}
	if cfg.adminToken == "" {
		return doctorConfig{}, fmt.Errorf("admin token is required; use --admin-token or --admin-token-stdin")
	}

	return cfg, nil
}

func normalizeDoctorURL(raw string, label string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid %s %q: %w", label, raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid %s %q: scheme and host are required", label, raw)
	}
	return strings.TrimRight(raw, "/"), nil
}

func runDoctorGET(ctx context.Context, client *http.Client, component string, name string, rawURL string, headers map[string]string, detailFn func(int, []byte) string) doctorCheck {
	start := time.Now()
	check := doctorCheck{
		Name:      name,
		Component: component,
		Method:    http.MethodGet,
		URL:       rawURL,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		check.DurationMS = time.Since(start).Milliseconds()
		check.Error = "build request: " + err.Error()
		return check
	}
	req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.1")
	req.Header.Set("User-Agent", "porthook/"+version+" doctor")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		check.DurationMS = time.Since(start).Milliseconds()
		check.Error = err.Error()
		return check
	}
	defer resp.Body.Close()

	body := []byte(readLimitedString(resp.Body, 4096))
	check.DurationMS = time.Since(start).Milliseconds()
	check.Status = resp.StatusCode
	check.RequestID = strings.TrimSpace(resp.Header.Get("X-Request-ID"))
	check.Detail = detailFn(resp.StatusCode, body)
	check.OK = resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices
	if !check.OK && check.Detail == "" {
		check.Detail = http.StatusText(resp.StatusCode)
	}
	return check
}

func skippedDoctorCheck(component string, name string, detail string) doctorCheck {
	return doctorCheck{
		Name:      name,
		Component: component,
		OK:        true,
		Skipped:   true,
		Detail:    detail,
	}
}

func (r *doctorReport) add(check doctorCheck) {
	r.Checks = append(r.Checks, check)
	if !check.OK {
		r.OK = false
	}
}

func (r doctorReport) failedCount() int {
	count := 0
	for _, check := range r.Checks {
		if !check.OK {
			count++
		}
	}
	return count
}

func plainDoctorDetail(_ int, body []byte) string {
	return compactDoctorBody(body)
}

func tunnelsDoctorDetail(_ int, body []byte) string {
	var listed doctorTunnelList
	if err := json.Unmarshal(body, &listed); err == nil {
		return fmt.Sprintf("tunnels=%d", len(listed.Tunnels))
	}
	return compactDoctorBody(body)
}

func controlPlaneStatusDoctorDetail(_ int, body []byte) string {
	var status doctorControlPlaneStatus
	if err := json.Unmarshal(body, &status); err == nil {
		parts := []string{}
		if status.Status != "" {
			parts = append(parts, "status="+status.Status)
		}
		parts = append(parts, fmt.Sprintf("ready=%t", status.Ready))
		if status.Version != "" {
			parts = append(parts, "version="+status.Version)
		}
		if status.Error != "" {
			parts = append(parts, "error="+status.Error)
		}
		return strings.Join(parts, " ")
	}
	return compactDoctorBody(body)
}

func auditEventsDoctorDetail(_ int, body []byte) string {
	var listed doctorAuditEventList
	if err := json.Unmarshal(body, &listed); err == nil {
		return fmt.Sprintf("events=%d", len(listed.Events))
	}
	return compactDoctorBody(body)
}

func runtimeDoctorDetail(_ int, body []byte) string {
	var payload doctorRuntimeResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		return fmt.Sprintf(
			"active_tunnels=%d active_streams=%d request_logs=%d/%d uptime=%s",
			payload.Runtime.ActiveTunnels,
			payload.Runtime.ActiveStreams,
			payload.Runtime.RequestLogEntries,
			payload.Runtime.RequestLogCapacity,
			formatTunnelDuration(time.Duration(payload.Runtime.UptimeSeconds)*time.Second),
		)
	}
	return compactDoctorBody(body)
}

func requestLogsDoctorDetail(_ int, body []byte) string {
	var listed doctorRequestLogList
	if err := json.Unmarshal(body, &listed); err == nil {
		return fmt.Sprintf("request_logs=%d", len(listed.RequestLogs))
	}
	return compactDoctorBody(body)
}

func metricsDoctorDetail(_ int, body []byte) string {
	count := 0
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		count++
	}
	return fmt.Sprintf("metrics=%d bytes=%d", count, len(body))
}

func compactDoctorBody(body []byte) string {
	value := strings.TrimSpace(string(body))
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 240 {
		value = value[:240] + "..."
	}
	return value
}

func printDoctorReport(w io.Writer, report doctorReport) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RESULT\tCOMPONENT\tCHECK\tSTATUS\tREQUEST ID\tDURATION\tDETAIL")
	for _, check := range report.Checks {
		result := "OK"
		status := "-"
		duration := "-"
		if check.Skipped {
			result = "SKIP"
		} else if !check.OK {
			result = "FAIL"
		}
		if check.Status != 0 {
			status = fmt.Sprintf("%d", check.Status)
		}
		if check.DurationMS != 0 || (!check.Skipped && check.Error == "") {
			duration = fmt.Sprintf("%dms", check.DurationMS)
		}
		detail := check.Detail
		if check.Error != "" {
			detail = check.Error
		}
		requestID := check.RequestID
		if requestID == "" {
			requestID = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", result, check.Component, check.Name, status, requestID, duration, detail)
	}
	_ = tw.Flush()
}

func printDoctorHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook doctor --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--timeout DURATION] [--json]

Run local operational checks against a self-hosted Porthook deployment.

Checks:
  gateway health, readiness, tunnels, runtime, request logs, and metrics through the operator API
  control-plane /healthz, /readyz, and /api/v1/status
  control-plane /api/v1/events when an admin token is set

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Control-plane admin token. Defaults to PORTHOOK_CONTROL_ADMIN_TOKEN.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --timeout DURATION        Per-check HTTP timeout. Defaults to 5s.
  --json                    Write JSON output.`)
}
