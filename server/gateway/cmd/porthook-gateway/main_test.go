// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	oldVersion := version
	version = "test-version"
	defer func() {
		version = oldVersion
	}()

	var stdout bytes.Buffer
	if err := run([]string{"--version"}, &stdout); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "test-version" {
		t.Fatalf("version output = %q, want test-version", got)
	}
}

func TestRunRejectsUnknownArgs(t *testing.T) {
	var stdout bytes.Buffer
	err := run([]string{"--unknown"}, &stdout)
	if err == nil {
		t.Fatal("run returned nil error")
	}
	if !strings.Contains(err.Error(), "usage: porthook-gateway") {
		t.Fatalf("error = %q, want usage guidance", err.Error())
	}
}

func TestRunHealthcheckUsesConfiguredURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Fatalf("path = %q, want /readyz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ready\n"))
	}))
	defer server.Close()
	t.Setenv("PORTHOOK_HEALTHCHECK_URL", server.URL+"/readyz")

	var stdout bytes.Buffer
	if err := run([]string{"healthcheck"}, &stdout); err != nil {
		t.Fatalf("run healthcheck returned error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "ok" {
		t.Fatalf("stdout = %q, want ok", got)
	}
}

func TestRunHealthcheckUsesManagementListener(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Fatalf("path = %q, want /readyz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ready\n"))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	t.Setenv("PORTHOOK_HEALTHCHECK_URL", "")
	t.Setenv("PORTHOOK_MANAGEMENT_ADDR", parsed.Host)

	var stdout bytes.Buffer
	if err := run([]string{"healthcheck"}, &stdout); err != nil {
		t.Fatalf("run healthcheck returned error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "ok" {
		t.Fatalf("stdout = %q, want ok", got)
	}
}

func TestRunHealthcheckReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("PORTHOOK_HEALTHCHECK_URL", server.URL)

	var stdout bytes.Buffer
	err := run([]string{"healthcheck"}, &stdout)
	if err == nil {
		t.Fatal("run healthcheck returned nil error")
	}
	if !strings.Contains(err.Error(), "healthcheck returned 503") {
		t.Fatalf("error = %q, want status guidance", err.Error())
	}
}

func TestRunConfigcheckAllowsDevelopmentDefaultsWithWarnings(t *testing.T) {
	var stdout bytes.Buffer
	if err := run([]string{"configcheck"}, &stdout); err != nil {
		t.Fatalf("run configcheck returned error: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "warning: PORTHOOK_STATIC_TOKEN") {
		t.Fatalf("stdout = %q, want static token warning", output)
	}
	if !strings.Contains(output, "ok") {
		t.Fatalf("stdout = %q, want ok", output)
	}
}

func TestRunConfigcheckReportsMissingControlPlaneToken(t *testing.T) {
	t.Setenv("PORTHOOK_CONTROL_PLANE_URL", "http://control-plane:8082")
	t.Setenv("PORTHOOK_CONTROL_PLANE_TOKEN", "")

	var stdout bytes.Buffer
	err := run([]string{"configcheck"}, &stdout)
	if err == nil {
		t.Fatal("run configcheck returned nil error")
	}
	output := stdout.String()
	if !strings.Contains(output, "error: PORTHOOK_CONTROL_PLANE_TOKEN") {
		t.Fatalf("stdout = %q, want control-plane token error", output)
	}
}

func TestRunConfigcheckProductionAcceptsControlPlaneConfig(t *testing.T) {
	t.Setenv("PORTHOOK_ROOT_DOMAIN", "tunnels.example.com")
	t.Setenv("PORTHOOK_PUBLIC_URL", "https://tunnels.example.com")
	t.Setenv("PORTHOOK_CONTROL_PLANE_URL", "http://control-plane:8082")
	t.Setenv("PORTHOOK_CONTROL_PLANE_TOKEN", "validator-secret")
	t.Setenv("PORTHOOK_MANAGEMENT_TOKEN", "management-secret")
	t.Setenv("PORTHOOK_REQUEST_LOG_DATABASE_URL", "postgres://porthook:secret@postgres:5432/porthook?sslmode=disable")

	var stdout bytes.Buffer
	if err := run([]string{"configcheck", "--production"}, &stdout); err != nil {
		t.Fatalf("run configcheck --production returned error: %v; stdout=%q", err, stdout.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "ok" {
		t.Fatalf("stdout = %q, want ok", got)
	}
}
