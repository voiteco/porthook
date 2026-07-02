// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunDoctorChecksGatewayAndControlPlane(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req_gateway")
		switch r.URL.Path {
		case "/healthz":
			if r.Method != http.MethodGet {
				t.Fatalf("gateway health method = %s, want GET", r.Method)
			}
			_, _ = w.Write([]byte("ok\n"))
		case "/readyz":
			_, _ = w.Write([]byte("ready\n"))
		case "/api/v1/tunnels":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tunnels":[{"tunnel_id":"tun_1"}]}`))
		case "/api/v1/runtime":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"runtime":{"active_tunnels":1,"active_streams":2,"request_log_entries":3,"request_log_capacity":100,"uptime_seconds":60}}`))
		case "/api/v1/request-logs":
			if r.URL.Query().Get("limit") != "1" {
				t.Fatalf("request logs limit = %q, want 1", r.URL.Query().Get("limit"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_logs":[{"request_id":"req_public"}]}`))
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("# TYPE porthook_gateway_active_tunnels gauge\nporthook_gateway_active_tunnels 1\n"))
		default:
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
	}))
	defer gateway.Close()

	var gotEventsAuth string
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req_control")
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte("ok\n"))
		case "/readyz":
			_, _ = w.Write([]byte("ready\n"))
		case "/api/v1/status":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(doctorControlPlaneStatus{
				Status:  "ready",
				Ready:   true,
				Version: "test-version",
			})
		case "/api/v1/events":
			if r.URL.Query().Get("limit") != "1" {
				t.Fatalf("events limit = %q, want 1", r.URL.Query().Get("limit"))
			}
			gotEventsAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"events":[{"event":"control_plane.token_created"}]}`))
		default:
			t.Fatalf("unexpected control-plane path %s", r.URL.Path)
		}
	}))
	defer controlPlane.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"doctor",
			"--gateway", gateway.URL,
			"--control-plane", controlPlane.URL,
			"--admin-token-stdin",
		},
		strings.NewReader("admin-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run doctor returned error: %v", err)
	}
	if gotEventsAuth != "Bearer admin-secret" {
		t.Fatalf("events Authorization = %q, want bearer admin token", gotEventsAuth)
	}

	output := stdout.String()
	for _, want := range []string{"OK", "gateway", "control-plane", "req_gateway", "req_control", "tunnels=1", "active_tunnels=1", "request_logs=1", "metrics=1", "events=1", "version=test-version"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunDoctorJSONReportsReadinessFailure(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req_ready_failed")
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte("ok\n"))
		case "/readyz":
			http.Error(w, "not ready: database unavailable", http.StatusServiceUnavailable)
		case "/api/v1/tunnels":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tunnels":[]}`))
		case "/api/v1/runtime":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"runtime":{"active_tunnels":0,"request_log_capacity":100}}`))
		case "/api/v1/request-logs":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_logs":[]}`))
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(""))
		default:
			t.Fatalf("unexpected gateway path %s", r.URL.Path)
		}
	}))
	defer gateway.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"doctor", "--gateway", gateway.URL, "--json"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run doctor returned nil error")
	}
	if !strings.Contains(err.Error(), "doctor found 1 failed check") {
		t.Fatalf("error = %q, want failed check count", err.Error())
	}

	var report doctorReport
	if err := json.NewDecoder(&stdout).Decode(&report); err != nil {
		t.Fatalf("decode stdout returned error: %v", err)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false")
	}
	var foundReadiness bool
	for _, check := range report.Checks {
		if check.Component == "gateway" && check.Name == "readiness" {
			foundReadiness = true
			if check.OK || check.Status != http.StatusServiceUnavailable {
				t.Fatalf("readiness check = %+v, want failed 503", check)
			}
			if check.RequestID != "req_ready_failed" {
				t.Fatalf("request ID = %q, want req_ready_failed", check.RequestID)
			}
			if !strings.Contains(check.Detail, "database unavailable") {
				t.Fatalf("detail = %q, want readiness body", check.Detail)
			}
		}
	}
	if !foundReadiness {
		t.Fatalf("checks = %+v, want gateway readiness check", report.Checks)
	}
}

func TestRunDoctorRejectsAdminTokenFlagAndStdin(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"doctor", "--gateway", "http://127.0.0.1:8080", "--admin-token", "admin-flag", "--admin-token-stdin"},
		strings.NewReader("admin-stdin"),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run doctor returned nil error")
	}
	if !strings.Contains(err.Error(), "--admin-token and --admin-token-stdin are mutually exclusive") {
		t.Fatalf("error = %q, want mutually exclusive guidance", err.Error())
	}
}
