// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voiteco/porthook/server/control-plane/internal/admintokens"
)

func TestGatewayOperatorForwardsAuthorizedRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer management-secret" {
			t.Errorf("Authorization = %q, want service bearer", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/test")
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprint(w, r.URL.RequestURI())
	}))
	defer upstream.Close()

	server := NewServer(Config{
		AdminToken:               "admin-secret",
		GatewayManagementURL:     upstream.URL,
		GatewayManagementToken:   "management-secret",
		GatewayManagementTimeout: time.Second,
	}, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	cases := []struct {
		operatorPath string
		upstreamPath string
	}{
		{operatorPath: "/healthz", upstreamPath: "/healthz"},
		{operatorPath: "/readyz", upstreamPath: "/readyz"},
		{operatorPath: "/metrics", upstreamPath: "/metrics"},
		{operatorPath: "/tunnels", upstreamPath: "/api/v1/tunnels"},
		{operatorPath: "/tunnels/tun_test", upstreamPath: "/api/v1/tunnels/tun_test"},
		{operatorPath: "/runtime", upstreamPath: "/api/v1/runtime"},
		{operatorPath: "/request-logs?limit=7", upstreamPath: "/api/v1/request-logs?limit=7"},
	}
	for _, tc := range cases {
		t.Run(tc.operatorPath, func(t *testing.T) {
			resp := getWithBearer(t, httpServer.Client(), httpServer.URL+gatewayOperatorPrefix+tc.operatorPath, "admin-secret")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/test" {
				t.Fatalf("Content-Type = %q, want application/test", got)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("ReadAll returned error: %v", err)
			}
			if string(body) != tc.upstreamPath {
				t.Fatalf("body = %q, want %q", body, tc.upstreamPath)
			}
		})
	}
}

func TestGatewayOperatorEnforcesScopedAdminTokens(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	adminService := admintokens.NewService(admintokens.NewMemoryStore())
	runtimeToken, err := adminService.CreateToken(context.Background(), admintokens.CreateTokenRequest{
		Name:   "runtime reader",
		Scopes: []string{admintokens.ScopeRuntimeDiagnostics},
	})
	if err != nil {
		t.Fatalf("create runtime token: %v", err)
	}
	auditToken, err := adminService.CreateToken(context.Background(), admintokens.CreateTokenRequest{
		Name:   "audit reader",
		Scopes: []string{admintokens.ScopeAuditHistory},
	})
	if err != nil {
		t.Fatalf("create audit token: %v", err)
	}

	server := NewServerWithAdminTokens(Config{
		GatewayManagementURL:     upstream.URL,
		GatewayManagementToken:   "management-secret",
		GatewayManagementTimeout: time.Second,
	}, nil, nil, nil, nil, nil, adminService)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	cases := []struct {
		name       string
		path       string
		token      string
		wantStatus int
	}{
		{name: "runtime tunnel inventory", path: "/tunnels", token: runtimeToken.Token, wantStatus: http.StatusOK},
		{name: "runtime denied audit", path: "/request-logs", token: runtimeToken.Token, wantStatus: http.StatusForbidden},
		{name: "audit request logs", path: "/request-logs", token: auditToken.Token, wantStatus: http.StatusOK},
		{name: "audit denied runtime", path: "/runtime", token: auditToken.Token, wantStatus: http.StatusForbidden},
		{name: "missing token", path: "/runtime", wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := calls.Load()
			resp := getWithBearer(t, httpServer.Client(), httpServer.URL+gatewayOperatorPrefix+tc.path, tc.token)
			resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			wantCalls := before
			if tc.wantStatus == http.StatusOK {
				wantCalls++
			}
			if got := calls.Load(); got != wantCalls {
				t.Fatalf("upstream calls = %d, want %d", got, wantCalls)
			}
		})
	}
}

func TestGatewayOperatorRejectsUnknownPaths(t *testing.T) {
	for _, path := range []string{"", "/unknown", "/tunnels/", "/tunnels/a/b"} {
		t.Run(strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			if _, _, ok := gatewayOperatorRoute(gatewayOperatorPrefix + path); ok {
				t.Fatalf("route %q was accepted", path)
			}
		})
	}
}
