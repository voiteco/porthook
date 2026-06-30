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

func TestRunAccessCreateUsesBasicPasswordStdin(t *testing.T) {
	createdAt := time.Date(2026, 6, 30, 14, 0, 0, 0, time.UTC)
	var gotAuth string
	var gotReq accessPolicyRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/access-policies" {
			t.Fatalf("request = %s %s, want POST /api/v1/access-policies", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accessPolicySummary{
			ID:                  "ap_created",
			ReservedSubdomainID: gotReq.ReservedSubdomainID,
			Mode:                gotReq.Mode,
			BasicUsername:       gotReq.BasicUsername,
			SecretConfigured:    true,
			CreatedAt:           createdAt,
			UpdatedAt:           createdAt,
		})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"access", "create",
			"--control-plane", server.URL,
			"--admin-token", "admin-secret",
			"--reserved-subdomain-id", "rs_demo",
			"--mode", "basic_auth",
			"--basic-username", "admin",
			"--basic-password-stdin",
			"--json",
		},
		strings.NewReader("basic-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run access create returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	if gotReq.ReservedSubdomainID != "rs_demo" || gotReq.Mode != "basic_auth" || gotReq.BasicUsername != "admin" || gotReq.BasicPassword != "basic-secret" {
		t.Fatalf("request = %+v, want basic auth policy request", gotReq)
	}

	var created accessPolicySummary
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout returned error: %v", err)
	}
	if created.ID != "ap_created" || !created.SecretConfigured {
		t.Fatalf("created = %+v, want created access policy JSON", created)
	}
	if strings.Contains(stdout.String(), "basic-secret") {
		t.Fatalf("stdout leaked policy secret: %q", stdout.String())
	}
}

func TestRunAccessListPrintsTable(t *testing.T) {
	updatedAt := time.Date(2026, 6, 30, 14, 0, 0, 0, time.UTC)
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/access-policies" {
			t.Fatalf("request = %s %s, want GET /api/v1/access-policies", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listAccessPoliciesResponse{AccessPolicies: []accessPolicySummary{{
			ID:                  "ap_listed",
			ReservedSubdomainID: "rs_demo",
			Mode:                "ip_allowlist",
			IPAllowlist:         []string{"192.0.2.0/24"},
			UpdatedAt:           updatedAt,
		}}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"access", "list", "--control-plane", server.URL, "--admin-token", "admin-secret"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run access list returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	output := stdout.String()
	for _, want := range []string{"ID", "RESERVED SUBDOMAIN ID", "MODE", "ap_listed", "rs_demo", "ip_allowlist", "192.0.2.0/24"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunAccessUpdateAndDelete(t *testing.T) {
	var gotAuth []string
	var updatedReq accessPolicyRequest
	var deletedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/access-policies/ap_update":
			if err := json.NewDecoder(r.Body).Decode(&updatedReq); err != nil {
				t.Fatalf("Decode update returned error: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(accessPolicySummary{
				ID:                  "ap_update",
				ReservedSubdomainID: "rs_demo",
				Mode:                updatedReq.Mode,
				SecretConfigured:    true,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/access-policies/ap_update":
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"access", "update", "--control-plane", server.URL, "--admin-token", "admin-secret", "--mode", "bearer_token", "--bearer-token-stdin", "ap_update"},
		strings.NewReader("bearer-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run access update returned error: %v", err)
	}
	if updatedReq.Mode != "bearer_token" || updatedReq.BearerToken != "bearer-secret" {
		t.Fatalf("updated request = %+v, want bearer token update", updatedReq)
	}
	stdout.Reset()

	err = runWithIO(
		[]string{"access", "delete", "--control-plane", server.URL, "--admin-token", "admin-secret", "ap_update"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run access delete returned error: %v", err)
	}
	if deletedPath != "/api/v1/access-policies/ap_update" {
		t.Fatalf("deleted path = %q, want ap_update", deletedPath)
	}
	for _, auth := range gotAuth {
		if auth != "Bearer admin-secret" {
			t.Fatalf("Authorization = %q, want bearer admin token", auth)
		}
	}
	if !strings.Contains(stdout.String(), "Access policy deleted: ap_update") {
		t.Fatalf("stdout = %q, want delete confirmation", stdout.String())
	}
}

func TestRunAccessHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"access", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run access help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"access create", "access list", "access update", "access delete"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunAccessRejectsMultipleStdinSecrets(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"access", "create",
			"--control-plane", "http://127.0.0.1:8082",
			"--admin-token-stdin",
			"--reserved-subdomain-id", "rs_demo",
			"--mode", "basic_auth",
			"--basic-username", "admin",
			"--basic-password-stdin",
		},
		strings.NewReader("secret"),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run access create returned nil error")
	}
	if !strings.Contains(err.Error(), "--admin-token-stdin cannot be combined") {
		t.Fatalf("error = %q, want stdin conflict guidance", err.Error())
	}
}
