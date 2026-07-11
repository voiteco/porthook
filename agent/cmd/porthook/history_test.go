// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunHistoryEventsListsAuditEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/events" {
			t.Fatalf("request = %s %s, want GET /api/v1/events", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer admin-secret" {
			t.Fatalf("Authorization = %q, want bearer admin token", got)
		}
		query := r.URL.Query()
		if got := query.Get("limit"); got != "2" {
			t.Fatalf("limit = %q, want 2", got)
		}
		if got := query.Get("event"); got != "token" {
			t.Fatalf("event = %q, want token", got)
		}
		if got := query.Get("field"); got != "tok_1" {
			t.Fatalf("field = %q, want tok_1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[{"time":"2026-07-02T10:00:00Z","level":"INFO","event":"control_plane.token_created","message":"created token","request_id":"req_1","remote_ip":"127.0.0.1","fields":{"token_id":"tok_1"}}],"next_cursor":"cur_2","filters":{"limit":2}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"history", "events", "--control-plane", server.URL, "--admin-token", "admin-secret", "--limit", "2", "--event", "token", "--field", "tok_1"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run history events returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"control_plane.token_created", "req_1", "token_id=tok_1", "Next cursor: cur_2"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunHistoryRequestsListsRequestLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/gateway/request-logs" {
			t.Fatalf("request = %s %s, want operator request logs endpoint", r.Method, r.URL.Path)
		}
		assertBearer(t, r, "admin-secret")
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "history") {
			t.Fatalf("User-Agent = %q, want history client", got)
		}
		query := r.URL.Query()
		if got := query.Get("limit"); got != "1" {
			t.Fatalf("limit = %q, want 1", got)
		}
		if got := query.Get("method"); got != "GET" {
			t.Fatalf("method = %q, want GET", got)
		}
		if got := query.Get("status"); got != "502" {
			t.Fatalf("status = %q, want 502", got)
		}
		if got := query.Get("cursor"); got != "cur_1" {
			t.Fatalf("cursor = %q, want cur_1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_logs":[{"time":"2026-07-02T10:00:00Z","method":"GET","host":"demo.localhost","path":"/demo","remote_ip":"127.0.0.1","request_id":"req_2","subdomain":"demo","status":502,"outcome":"upstream_error","duration_ms":125}],"next_cursor":"cur_2","filters":{"limit":1}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"history", "requests", "--control-plane", server.URL, "--admin-token-stdin", "--limit", "1", "--method", "get", "--status", "502", "--cursor", "cur_1"},
		strings.NewReader("admin-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run history requests returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"demo", "GET", "502", "upstream_error", "req_2", "Next cursor: cur_2"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunHistoryEventsRejectsInvalidTimeWindow(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"history", "events", "--control-plane", "http://127.0.0.1:8082", "--admin-token", "admin-secret", "--since", "2026-07-02T11:00:00Z", "--until", "2026-07-02T10:00:00Z"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run history events returned nil error")
	}
	if !strings.Contains(err.Error(), "since must be before until") {
		t.Fatalf("error = %q, want time window guidance", err.Error())
	}
}

func TestRunHistoryRequestsRejectsInvalidStatus(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"history", "requests", "--control-plane", "http://127.0.0.1:8082", "--admin-token", "admin-secret", "--status", "42"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run history requests returned nil error")
	}
	if !strings.Contains(err.Error(), "status must be between 100 and 599") {
		t.Fatalf("error = %q, want status guidance", err.Error())
	}
}

func TestRunHistoryHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"history", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run history help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"porthook history events", "porthook history requests"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}
