// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunOperationalExportWritesSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	var authorizedAdminCalls int
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(doctorControlPlaneStatus{Status: "ok", Ready: true, Version: "test"})
		case "/api/v1/tokens":
			requireAdminAuth(t, r, &authorizedAdminCalls)
			_ = json.NewEncoder(w).Encode(listTokensResponse{Tokens: []tokenSummary{{ID: "tok_demo", Name: "demo", Scopes: []string{"register_tunnel"}, CreatedAt: now}}})
		case "/api/v1/reserved-subdomains":
			requireAdminAuth(t, r, &authorizedAdminCalls)
			_ = json.NewEncoder(w).Encode(listReservationsResponse{ReservedSubdomains: []reservationSummary{{ID: "rs_demo", Name: "demo", TokenID: "tok_demo", CreatedAt: now}}})
		case "/api/v1/custom-domains":
			requireAdminAuth(t, r, &authorizedAdminCalls)
			_ = json.NewEncoder(w).Encode(listCustomDomainsResponse{CustomDomains: []customDomainSummary{{ID: "cd_demo", Hostname: "preview.example.test", ReservedSubdomainID: "rs_demo", Status: "active", CreatedAt: now, UpdatedAt: now}}})
		case "/api/v1/access-policies":
			requireAdminAuth(t, r, &authorizedAdminCalls)
			_ = json.NewEncoder(w).Encode(listAccessPoliciesResponse{AccessPolicies: []accessPolicySummary{{ID: "ap_demo", ReservedSubdomainID: "rs_demo", Mode: "public", CreatedAt: now, UpdatedAt: now}}})
		case "/api/v1/events":
			requireAdminAuth(t, r, &authorizedAdminCalls)
			if got := r.URL.Query().Get("limit"); got != "5" {
				t.Fatalf("event limit = %q, want 5", got)
			}
			_, _ = w.Write([]byte(`{"events":[{"event":"control_plane.token_created"}]}`))
		default:
			t.Fatalf("unexpected control-plane request: %s", r.URL.Path)
		}
	}))
	defer controlPlane.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tunnels":
			_ = json.NewEncoder(w).Encode(listTunnelsResponse{Tunnels: []tunnelSummaryCLI{{TunnelID: "tun_demo", Subdomain: "demo", PublicURL: "http://demo.example.test", Protocol: "http", ConnectedAt: now}}})
		case "/api/v1/tunnels/tun_demo":
			_ = json.NewEncoder(w).Encode(showTunnelResponse{Tunnel: tunnelDetailCLI{TunnelID: "tun_demo", Subdomain: "demo", PublicURL: "http://demo.example.test", Protocol: "http", ConnectedAt: now, ConnectedSeconds: 60, StreamCapacity: 4}})
		case "/api/v1/runtime":
			_, _ = w.Write([]byte(`{"runtime":{"active_tunnels":1,"stream_capacity":4}}`))
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("# HELP porthook_gateway_active_tunnels Active tunnels\n# TYPE porthook_gateway_active_tunnels gauge\nporthook_gateway_active_tunnels 1\n"))
		case "/api/v1/request-logs":
			if got := r.URL.Query().Get("limit"); got != "7" {
				t.Fatalf("request log limit = %q, want 7", got)
			}
			_ = json.NewEncoder(w).Encode(operationalExportRequestLogsResponse{RequestLogs: []operationalExportRequestLog{{Time: now, Method: http.MethodGet, Host: "demo.example.test", Path: "/", RemoteIP: "192.0.2.1", RequestID: "req_demo", Subdomain: "demo", TunnelID: "tun_demo", Status: http.StatusOK, Outcome: "completed", DurationMS: 12}}})
		default:
			t.Fatalf("unexpected gateway request: %s", r.URL.Path)
		}
	}))
	defer gateway.Close()

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{
		"export",
		"--control-plane", controlPlane.URL,
		"--gateway", gateway.URL,
		"--admin-token", "admin",
		"--event-limit", "5",
		"--request-log-limit", "7",
	}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run export returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if authorizedAdminCalls != 5 {
		t.Fatalf("authorized admin calls = %d, want 5", authorizedAdminCalls)
	}
	var snapshot operationalExportSnapshot
	if err := json.NewDecoder(&stdout).Decode(&snapshot); err != nil {
		t.Fatalf("decode snapshot returned error: %v", err)
	}
	if len(snapshot.Errors) != 0 {
		t.Fatalf("errors = %+v, want none", snapshot.Errors)
	}
	if snapshot.ControlPlane == nil || len(snapshot.ControlPlane.Tokens) != 1 || len(snapshot.ControlPlane.AuditEvents) != 1 {
		t.Fatalf("control plane snapshot = %+v, want token and audit event", snapshot.ControlPlane)
	}
	if snapshot.Gateway == nil || len(snapshot.Gateway.Tunnels) != 1 || len(snapshot.Gateway.TunnelDetails) != 1 || len(snapshot.Gateway.RequestLogs) != 1 {
		t.Fatalf("gateway snapshot = %+v, want tunnel detail and request log", snapshot.Gateway)
	}
	if len(snapshot.Gateway.Metrics) != 1 || snapshot.Gateway.Metrics[0].Name != "porthook_gateway_active_tunnels" {
		t.Fatalf("metrics = %+v, want active tunnels metric", snapshot.Gateway.Metrics)
	}
	if snapshot.Sources.EventLimit != 5 || snapshot.Sources.RequestLogLimit != 7 {
		t.Fatalf("sources = %+v, want configured limits", snapshot.Sources)
	}
}

func TestRunOperationalExportRecordsPartialErrors(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tunnels":
			http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)
		case "/api/v1/runtime", "/metrics", "/api/v1/request-logs":
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected gateway request: %s", r.URL.Path)
		}
	}))
	defer gateway.Close()

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{"export", "--gateway", gateway.URL, "--control-plane", ""}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run export returned error: %v", err)
	}
	var snapshot operationalExportSnapshot
	if err := json.NewDecoder(&stdout).Decode(&snapshot); err != nil {
		t.Fatalf("decode snapshot returned error: %v", err)
	}
	if len(snapshot.Errors) == 0 {
		t.Fatal("snapshot errors = 0, want partial error")
	}
	if snapshot.Gateway == nil {
		t.Fatal("gateway snapshot is nil, want partial gateway data")
	}
}

func TestRunOperationalExportWritesOutputFile(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tunnels":
			_, _ = w.Write([]byte(`{"tunnels":[]}`))
		case "/api/v1/runtime":
			_, _ = w.Write([]byte(`{"runtime":{}}`))
		case "/metrics":
			_, _ = w.Write([]byte(""))
		case "/api/v1/request-logs":
			_, _ = w.Write([]byte(`{"request_logs":[]}`))
		default:
			t.Fatalf("unexpected gateway request: %s", r.URL.Path)
		}
	}))
	defer gateway.Close()

	outputPath := filepath.Join(t.TempDir(), "porthook-export.json")
	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{"export", "--gateway", gateway.URL, "--control-plane", "", "--output", outputPath}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run export returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Wrote operational export") {
		t.Fatalf("stdout = %q, want output file message", stdout.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file returned error: %v", err)
	}
	var snapshot operationalExportSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("unmarshal output file returned error: %v", err)
	}
	if snapshot.Gateway == nil {
		t.Fatal("gateway snapshot is nil, want gateway data")
	}
}

func requireAdminAuth(t *testing.T, r *http.Request, count *int) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer admin" {
		t.Fatalf("Authorization = %q, want Bearer admin", got)
	}
	(*count)++
}
