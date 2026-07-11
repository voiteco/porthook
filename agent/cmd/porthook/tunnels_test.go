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

func TestRunTunnelsListPrintsActiveTunnels(t *testing.T) {
	connectedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/gateway/tunnels" {
			t.Fatalf("request = %s %s, want operator tunnels endpoint", r.Method, r.URL.Path)
		}
		assertBearer(t, r, "admin-secret")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listTunnelsResponse{Tunnels: []tunnelSummaryCLI{{
			TunnelID:    "tun_demo",
			Subdomain:   "demo",
			PublicURL:   "http://demo.localhost:8080",
			Protocol:    "http",
			ConnectedAt: connectedAt,
		}}})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{"tunnels", "list", "--control-plane", server.URL, "--admin-token", "admin-secret"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run tunnels list returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"SUBDOMAIN", "demo", "tun_demo", "http://demo.localhost:8080", connectedAt.Format(time.RFC3339)} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunTunnelsListWritesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/gateway/tunnels" {
			t.Fatalf("request = %s %s, want operator tunnels endpoint", r.Method, r.URL.Path)
		}
		assertBearer(t, r, "admin-secret")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listTunnelsResponse{Tunnels: []tunnelSummaryCLI{{
			TunnelID:  "tun_json",
			Subdomain: "json",
			PublicURL: "http://json.localhost:8080",
			Protocol:  "http",
		}}})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{"tunnels", "list", "--control-plane", server.URL, "--admin-token-stdin", "--json"}, strings.NewReader("admin-secret\n"), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run tunnels list returned error: %v", err)
	}
	var listed listTunnelsResponse
	if err := json.NewDecoder(&stdout).Decode(&listed); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(listed.Tunnels) != 1 || listed.Tunnels[0].TunnelID != "tun_json" {
		t.Fatalf("listed = %+v, want tun_json", listed)
	}
}

func TestRunTunnelsShowPrintsDetail(t *testing.T) {
	connectedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	lastRequestAt := connectedAt.Add(time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/gateway/tunnels/tun_demo" {
			t.Fatalf("request = %s %s, want operator tunnel detail endpoint", r.Method, r.URL.Path)
		}
		assertBearer(t, r, "admin-secret")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(showTunnelResponse{Tunnel: tunnelDetailCLI{
			TunnelID:         "tun_demo",
			Subdomain:        "demo",
			PublicURL:        "http://demo.localhost:8080",
			Protocol:         "http",
			AgentVersion:     "porthook/test",
			ProtocolVersion:  "0.2",
			ConnectedAt:      connectedAt,
			ConnectedSeconds: 90,
			ActiveStreams:    1,
			StreamCapacity:   4,
			RecentRequests: tunnelRequestRecent{
				Count:         3,
				LastRequestAt: &lastRequestAt,
				LastStatus:    http.StatusOK,
				LastOutcome:   "completed",
				LastRequestID: "req_demo",
				ErrorCount:    1,
				CustomDomains: []string{"preview.example.test"},
			},
		}})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{"tunnels", "show", "--control-plane", server.URL, "--admin-token", "admin-secret", "tun_demo"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("run tunnels show returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"Tunnel: tun_demo", "Subdomain: demo", "Streams: 1/4", "Recent requests: 3", "status=200", "req_demo", "preview.example.test"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunTunnelsHelpPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runWithIO([]string{"tunnels", "help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run tunnels help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"tunnels list", "tunnels show", "--control-plane", "--admin-token-stdin"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunTunnelsReturnsGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "tunnel not found", http.StatusNotFound)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := runWithIO([]string{"tunnels", "show", "--control-plane", server.URL, "--admin-token", "admin-secret", "missing"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("run tunnels show returned nil error")
	}
	if !strings.Contains(err.Error(), "gateway returned status 404") {
		t.Fatalf("error = %q, want gateway status", err)
	}
}

func assertBearer(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
}
