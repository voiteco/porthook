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

func TestRunTokensCreateUsesAdminTokenStdin(t *testing.T) {
	createdAt := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	var gotAuth string
	var gotReq createTokenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tokens" {
			t.Fatalf("request = %s %s, want POST /api/v1/tokens", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(createdToken{
			ID:        "tok_created",
			Name:      gotReq.Name,
			Token:     "ph_created",
			Scopes:    gotReq.Scopes,
			CreatedAt: createdAt,
		})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"tokens", "create",
			"--control-plane", server.URL,
			"--admin-token-stdin",
			"--name", "local agent",
			"--scope", "register_tunnel",
			"--json",
		},
		strings.NewReader("admin-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run tokens create returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	if gotReq.Name != "local agent" || len(gotReq.Scopes) != 1 || gotReq.Scopes[0] != "register_tunnel" {
		t.Fatalf("request = %+v, want name and scope", gotReq)
	}

	var created createdToken
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout returned error: %v", err)
	}
	if created.Token != "ph_created" || created.ID != "tok_created" {
		t.Fatalf("created = %+v, want created token JSON", created)
	}
}

func TestRunTokensListPrintsTable(t *testing.T) {
	createdAt := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	revokedAt := createdAt.Add(time.Minute)
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/tokens" {
			t.Fatalf("request = %s %s, want GET /api/v1/tokens", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listTokensResponse{Tokens: []tokenSummary{{
			ID:        "tok_listed",
			Name:      "listed agent",
			Scopes:    []string{"register_tunnel"},
			CreatedAt: createdAt,
			RevokedAt: &revokedAt,
		}}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"tokens", "list", "--control-plane", server.URL, "--admin-token", "admin-secret"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run tokens list returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	output := stdout.String()
	for _, want := range []string{"ID", "tok_listed", "listed agent", "register_tunnel", revokedAt.Format(time.RFC3339)} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunTokensRevokeUsesTokenID(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/tokens/tok_revoke" {
			t.Fatalf("request = %s %s, want DELETE /api/v1/tokens/tok_revoke", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"tokens", "revoke", "--control-plane", server.URL, "--admin-token", "admin-secret", "tok_revoke"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run tokens revoke returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	if !strings.Contains(stdout.String(), "Token revoked: tok_revoke") {
		t.Fatalf("stdout = %q, want revoke confirmation", stdout.String())
	}
}

func TestRunTokensRequiresAdminTokenWhenNonInteractive(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"tokens", "list", "--control-plane", "http://127.0.0.1:8082"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run tokens list returned nil error")
	}
	if !strings.Contains(err.Error(), "--admin-token-stdin") {
		t.Fatalf("error = %q, want admin-token-stdin guidance", err.Error())
	}
}
