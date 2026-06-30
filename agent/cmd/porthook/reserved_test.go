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

func TestRunReservedCreateUsesAdminTokenStdin(t *testing.T) {
	createdAt := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	var gotAuth string
	var gotReq createReservationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/reserved-subdomains" {
			t.Fatalf("request = %s %s, want POST /api/v1/reserved-subdomains", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(createdReservation{
			ID:        "rs_created",
			Name:      gotReq.Name,
			TokenID:   gotReq.TokenID,
			CreatedAt: createdAt,
		})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"reserved", "create",
			"--control-plane", server.URL,
			"--admin-token-stdin",
			"--name", "demo",
			"--token-id", "tok_owner",
			"--json",
		},
		strings.NewReader("admin-secret\n"),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run reserved create returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	if gotReq.Name != "demo" || gotReq.TokenID != "tok_owner" {
		t.Fatalf("request = %+v, want name and token_id", gotReq)
	}

	var created createdReservation
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout returned error: %v", err)
	}
	if created.ID != "rs_created" || created.TokenID != "tok_owner" {
		t.Fatalf("created = %+v, want created reservation JSON", created)
	}
}

func TestRunReservedListPrintsTable(t *testing.T) {
	createdAt := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/reserved-subdomains" {
			t.Fatalf("request = %s %s, want GET /api/v1/reserved-subdomains", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listReservationsResponse{ReservedSubdomains: []reservationSummary{{
			ID:        "rs_listed",
			Name:      "demo",
			TokenID:   "tok_owner",
			CreatedAt: createdAt,
		}}})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"reserved", "list", "--control-plane", server.URL, "--admin-token", "admin-secret"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run reserved list returned error: %v", err)
	}
	if gotAuth != "Bearer admin-secret" {
		t.Fatalf("Authorization = %q, want bearer admin token", gotAuth)
	}
	output := stdout.String()
	for _, want := range []string{"ID", "NAME", "TOKEN ID", "CREATED", "rs_listed", "demo", "tok_owner", createdAt.Format(time.RFC3339)} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunReservedDeleteResolvesName(t *testing.T) {
	var gotAuth []string
	var deletedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/reserved-subdomains":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(listReservationsResponse{ReservedSubdomains: []reservationSummary{{
				ID:      "rs_delete",
				Name:    "demo",
				TokenID: "tok_owner",
			}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/reserved-subdomains/rs_delete":
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
		[]string{"reserved", "delete", "--control-plane", server.URL, "--admin-token", "admin-secret", "demo"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run reserved delete returned error: %v", err)
	}
	if deletedPath != "/api/v1/reserved-subdomains/rs_delete" {
		t.Fatalf("deleted path = %q, want rs_delete", deletedPath)
	}
	for _, auth := range gotAuth {
		if auth != "Bearer admin-secret" {
			t.Fatalf("Authorization = %q, want bearer admin token", auth)
		}
	}
	if !strings.Contains(stdout.String(), "Reserved subdomain deleted: demo (rs_delete)") {
		t.Fatalf("stdout = %q, want delete confirmation", stdout.String())
	}
}

func TestRunReservedHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"reserved", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run reserved help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"reserved create", "reserved list", "reserved delete"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunReservedReportsConflictClearly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "reserved subdomain already exists", http.StatusConflict)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{
			"reserved", "create",
			"--control-plane", server.URL,
			"--admin-token", "admin-secret",
			"--name", "demo",
			"--token-id", "tok_owner",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run reserved create returned nil error")
	}
	if !strings.Contains(err.Error(), "control plane conflict: reserved subdomain already exists") {
		t.Fatalf("error = %q, want conflict guidance", err.Error())
	}
}
