// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunAdminTokensCreateUsesAdminTokenStdin(t *testing.T) {
	createdAt := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	var gotAuth string
	var gotReq createAdminTokenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin-tokens" {
			t.Fatalf("request = %s %s, want POST /api/v1/admin-tokens", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(createdAdminToken{
			ID:        "adm_created",
			Name:      gotReq.Name,
			Token:     "pat_created",
			Scopes:    gotReq.Scopes,
			CreatedAt: createdAt,
		})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"admin", "tokens", "create",
			"--control-plane", server.URL,
			"--admin-token-stdin",
			"--name", "audit reader",
			"--scope", "audit_history",
			"--json",
		},
		strings.NewReader("bootstrap-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run admin tokens create returned error: %v", err)
	}
	if gotAuth != "Bearer bootstrap-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	if gotReq.Name != "audit reader" || len(gotReq.Scopes) != 1 || gotReq.Scopes[0] != "audit_history" {
		t.Fatalf("request = %+v, want name and scope", gotReq)
	}

	var created createdAdminToken
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout returned error: %v", err)
	}
	if created.Token != "pat_created" || created.ID != "adm_created" {
		t.Fatalf("created = %+v, want created admin token JSON", created)
	}
}

func TestRunAdminHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"admin", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run admin help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"admin tokens", "admin help"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunAdminTokensHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"admin", "tokens", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run admin tokens help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"admin tokens create", "admin tokens list", "admin tokens revoke"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunAdminTokensCreateHelpPrintsSupportedScopes(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"admin", "tokens", "create", "--help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run admin tokens create help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"admin_tokens", "audit_history", "runtime_diagnostics", "--admin-token-stdin", "--json"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunAdminTokensListPrintsTable(t *testing.T) {
	createdAt := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	lastUsedAt := createdAt.Add(30 * time.Minute)
	revokedAt := createdAt.Add(time.Minute)
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin-tokens" {
			t.Fatalf("request = %s %s, want GET /api/v1/admin-tokens", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listAdminTokensResponse{Tokens: []adminTokenSummary{{
			ID:         "adm_listed",
			Name:       "listed admin",
			Scopes:     []string{"admin_tokens", "audit_history"},
			CreatedAt:  createdAt,
			LastUsedAt: &lastUsedAt,
			RevokedAt:  &revokedAt,
		}}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"admin", "tokens", "list", "--control-plane", server.URL, "--admin-token", "admin-secret"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run admin tokens list returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	output := stdout.String()
	for _, want := range []string{"ID", "LAST USED", "adm_listed", "listed admin", "admin_tokens,audit_history", lastUsedAt.Format(time.RFC3339), revokedAt.Format(time.RFC3339)} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunAdminTokensRevokeUsesAdminTokenID(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/admin-tokens/adm_revoke" {
			t.Fatalf("request = %s %s, want DELETE /api/v1/admin-tokens/adm_revoke", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"admin", "tokens", "revoke", "--control-plane", server.URL, "--admin-token", "admin-secret", "adm_revoke"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run admin tokens revoke returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	if !strings.Contains(stdout.String(), "Admin token revoked: adm_revoke") {
		t.Fatalf("stdout = %q, want revoke confirmation", stdout.String())
	}
}

func TestRunAdminTokensReportsForbiddenScopeClearly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden: admin token is missing a required scope", http.StatusForbidden)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"admin", "tokens", "list", "--control-plane", server.URL, "--admin-token", "limited"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run admin tokens list returned nil error")
	}
	if !strings.Contains(err.Error(), "missing the required admin scope") {
		t.Fatalf("error = %q, want scope guidance", err.Error())
	}
}
