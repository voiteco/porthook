// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voiteco/porthook/server/control-plane/internal/tokens"
)

func TestHealthEndpoints(t *testing.T) {
	server := NewServer(Config{}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := httpServer.Client().Get(httpServer.URL + path)
		if err != nil {
			t.Fatalf("GET %s returned error: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestTokenLifecycle(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", map[string]any{
		"name":   "agent",
		"scopes": []string{tokens.ScopeRegisterTunnel},
	})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", createResp.StatusCode)
	}

	var created tokens.CreatedToken
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created token returned error: %v", err)
	}
	if created.ID == "" || created.Token == "" {
		t.Fatalf("created token missing id or token: %+v", created)
	}

	validateResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "", map[string]any{
		"token": created.Token,
		"scope": tokens.ScopeRegisterTunnel,
	})
	defer validateResp.Body.Close()
	if validateResp.StatusCode != http.StatusOK {
		t.Fatalf("validate status = %d, want 200", validateResp.StatusCode)
	}
	var validation tokens.ValidationResult
	if err := json.NewDecoder(validateResp.Body).Decode(&validation); err != nil {
		t.Fatalf("decode validation returned error: %v", err)
	}
	if !validation.Valid || validation.TokenID != created.ID {
		t.Fatalf("validation = %+v, want valid token %q", validation, created.ID)
	}

	revokeReq, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, httpServer.URL+"/api/v1/tokens/"+created.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	revokeReq.Header.Set("Authorization", "Bearer admin-secret")
	revokeResp, err := httpServer.Client().Do(revokeReq)
	if err != nil {
		t.Fatalf("revoke returned error: %v", err)
	}
	revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", revokeResp.StatusCode)
	}

	validateResp = postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "", map[string]any{
		"token": created.Token,
		"scope": tokens.ScopeRegisterTunnel,
	})
	defer validateResp.Body.Close()
	if validateResp.StatusCode != http.StatusOK {
		t.Fatalf("validate revoked status = %d, want 200", validateResp.StatusCode)
	}
	validation = tokens.ValidationResult{}
	if err := json.NewDecoder(validateResp.Body).Decode(&validation); err != nil {
		t.Fatalf("decode revoked validation returned error: %v", err)
	}
	if validation.Valid {
		t.Fatal("revoked token validated")
	}
}

func TestCreateTokenRequiresAdminAuthorization(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "", map[string]any{
		"name": "agent",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateTokenRequiresConfiguredAdminToken(t *testing.T) {
	server := NewServer(Config{}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "", map[string]any{
		"name": "agent",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func postJSON(t *testing.T, client *http.Client, url, bearer string, payload any) *http.Response {
	t.Helper()

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	return resp
}
