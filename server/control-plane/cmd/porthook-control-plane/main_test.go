// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	version = "test-version"
	var stdout bytes.Buffer

	if err := run([]string{"version"}, &stdout); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "test-version" {
		t.Fatalf("stdout = %q, want test-version", got)
	}
}

func TestRunRejectsUnknownArgs(t *testing.T) {
	var stdout bytes.Buffer

	err := run([]string{"serve"}, &stdout)
	if err == nil {
		t.Fatal("run returned nil error")
	}
	if !strings.Contains(err.Error(), "usage: porthook-control-plane") {
		t.Fatalf("error = %q, want usage", err.Error())
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
