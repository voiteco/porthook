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

func TestRunDomainsCreateUsesAdminTokenStdin(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	var gotAuth string
	var gotReq createCustomDomainRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/custom-domains" {
			t.Fatalf("request = %s %s, want POST /api/v1/custom-domains", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(customDomainSummary{
			ID:                  "cd_created",
			Hostname:            "preview.example.test",
			ReservedSubdomainID: gotReq.ReservedSubdomainID,
			Status:              "active",
			CreatedAt:           now,
			UpdatedAt:           now,
		})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"domains", "create",
			"--control-plane", server.URL,
			"--admin-token-stdin",
			"--hostname", "preview.example.test",
			"--reserved-subdomain-id", "rs_demo",
			"--json",
		},
		strings.NewReader("admin-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run domains create returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	if gotReq.Hostname != "preview.example.test" || gotReq.ReservedSubdomainID != "rs_demo" {
		t.Fatalf("request = %+v, want hostname and reservation", gotReq)
	}

	var created customDomainSummary
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout returned error: %v", err)
	}
	if created.ID != "cd_created" || created.Hostname != "preview.example.test" {
		t.Fatalf("created = %+v, want created custom domain JSON", created)
	}
}

func TestRunDomainsListPrintsTable(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/custom-domains" {
			t.Fatalf("request = %s %s, want GET /api/v1/custom-domains", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listCustomDomainsResponse{CustomDomains: []customDomainSummary{{
			ID:                  "cd_listed",
			Hostname:            "preview.example.test",
			ReservedSubdomainID: "rs_demo",
			Status:              "active",
			UpdatedAt:           now,
		}}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"domains", "list", "--control-plane", server.URL, "--admin-token", "admin-secret"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run domains list returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	output := stdout.String()
	for _, want := range []string{"ID", "HOSTNAME", "RESERVED SUBDOMAIN ID", "STATUS", "UPDATED", "cd_listed", "preview.example.test", "rs_demo", "active", now.Format(time.RFC3339)} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunDomainsDeleteResolvesHostname(t *testing.T) {
	var gotAuth []string
	var deletedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/custom-domains":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(listCustomDomainsResponse{CustomDomains: []customDomainSummary{{
				ID:       "cd_delete",
				Hostname: "preview.example.test",
				Status:   "active",
			}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/custom-domains/cd_delete":
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
		[]string{"domains", "delete", "--control-plane", server.URL, "--admin-token", "admin-secret", "Preview.Example.Test."},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run domains delete returned error: %v", err)
	}
	if deletedPath != "/api/v1/custom-domains/cd_delete" {
		t.Fatalf("deleted path = %q, want cd_delete", deletedPath)
	}
	for _, auth := range gotAuth {
		if auth != "Bearer admin-secret" {
			t.Fatalf("Authorization = %q, want bearer admin token", auth)
		}
	}
	if !strings.Contains(stdout.String(), "Custom domain deleted: preview.example.test (cd_delete)") {
		t.Fatalf("stdout = %q, want delete confirmation", stdout.String())
	}
}

func TestRunDomainsHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"domains", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run domains help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"domains create", "domains list", "domains delete"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunDomainsReportsConflictClearly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "custom domain already exists", http.StatusConflict)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"domains", "create",
			"--control-plane", server.URL,
			"--admin-token", "admin-secret",
			"--hostname", "preview.example.test",
			"--reserved-subdomain-id", "rs_demo",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run domains create returned nil error")
	}
	if !strings.Contains(err.Error(), "control plane conflict: custom domain already exists") {
		t.Fatalf("error = %q, want conflict guidance", err.Error())
	}
}
