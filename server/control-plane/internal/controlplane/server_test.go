// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/voiteco/porthook/server/control-plane/internal/reserved"
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

func TestDashboardEndpoint(t *testing.T) {
	server := NewServer(Config{}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/dashboard/")
	if err != nil {
		t.Fatalf("GET dashboard returned error: %v", err)
	}
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Token management") {
		t.Fatalf("body = %q, want dashboard token management UI", body)
	}

	resp, err = httpServer.Client().Get(httpServer.URL + "/dashboard/app.js")
	if err != nil {
		t.Fatalf("GET dashboard app returned error: %v", err)
	}
	defer resp.Body.Close()
	body = readResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "/api/v1/tokens") {
		t.Fatalf("asset body = %q, want token API client", body)
	}
}

func TestReadyzReportsStoreFailure(t *testing.T) {
	server := NewServer(Config{}, tokens.NewService(failingReadyStore{MemoryStore: tokens.NewMemoryStore()}))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET readyz returned error: %v", err)
	}
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "not ready") {
		t.Fatalf("body = %q, want readiness guidance", body)
	}
}

func TestStatusEndpoint(t *testing.T) {
	server := NewServer(Config{Version: "test-version"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("GET status returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status returned error: %v", err)
	}
	if !status.Ready || status.Status != "ready" || status.Version != "test-version" {
		t.Fatalf("status response = %+v, want ready test-version", status)
	}
}

func TestStatusEndpointReportsStoreFailure(t *testing.T) {
	server := NewServer(Config{Version: "test-version"}, tokens.NewService(failingReadyStore{MemoryStore: tokens.NewMemoryStore()}))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("GET status returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status returned error: %v", err)
	}
	if status.Ready || status.Status != "not_ready" || status.Error == "" {
		t.Fatalf("status response = %+v, want not_ready error", status)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	server := NewServer(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", map[string]any{
		"name": "agent",
	})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", createResp.StatusCode)
	}
	var created tokens.CreatedToken
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created token returned error: %v", err)
	}

	validateResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "validator-secret", map[string]any{
		"token": created.Token,
		"scope": tokens.ScopeRegisterTunnel,
	})
	validateResp.Body.Close()
	if validateResp.StatusCode != http.StatusOK {
		t.Fatalf("validate status = %d, want 200", validateResp.StatusCode)
	}

	resp, err := httpServer.Client().Get(httpServer.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET metrics returned error: %v", err)
	}
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200; body = %q", resp.StatusCode, body)
	}
	for _, want := range []string{
		"porthook_control_plane_token_admin_creates_total 1",
		"porthook_control_plane_token_validations_total 1",
		"porthook_control_plane_token_validation_valid_total 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics = %q, want %q", body, want)
		}
	}
}

func TestTokenLifecycle(t *testing.T) {
	var logs bytes.Buffer
	server := NewServer(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()))
	server.logger = slog.New(slog.NewTextHandler(&logs, nil))
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

	listReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/tokens", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer admin-secret")
	listResp, err := httpServer.Client().Do(listReq)
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
	var listed tokens.ListTokensResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode token list returned error: %v", err)
	}
	if len(listed.Tokens) != 1 {
		t.Fatalf("listed tokens = %d, want 1", len(listed.Tokens))
	}
	if listed.Tokens[0].ID != created.ID || listed.Tokens[0].Name != created.Name {
		t.Fatalf("listed token = %+v, want created token summary", listed.Tokens[0])
	}
	if listed.Tokens[0].RevokedAt != nil {
		t.Fatalf("listed token revoked_at = %v, want nil", listed.Tokens[0].RevokedAt)
	}
	if listed.Tokens[0].LastUsedAt != nil {
		t.Fatalf("listed token last_used_at = %v, want nil before validation", listed.Tokens[0].LastUsedAt)
	}

	validateResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "validator-secret", map[string]any{
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

	listReq, err = http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/tokens", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer admin-secret")
	listResp, err = httpServer.Client().Do(listReq)
	if err != nil {
		t.Fatalf("list used returned error: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list used status = %d, want 200", listResp.StatusCode)
	}
	listed = tokens.ListTokensResponse{}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode used token list returned error: %v", err)
	}
	if len(listed.Tokens) != 1 || listed.Tokens[0].LastUsedAt == nil {
		t.Fatalf("listed used tokens = %+v, want last_used_at", listed.Tokens)
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

	listReq, err = http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/tokens", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer admin-secret")
	listResp, err = httpServer.Client().Do(listReq)
	if err != nil {
		t.Fatalf("list revoked returned error: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list revoked status = %d, want 200", listResp.StatusCode)
	}
	listed = tokens.ListTokensResponse{}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode revoked token list returned error: %v", err)
	}
	if len(listed.Tokens) != 1 || listed.Tokens[0].RevokedAt == nil {
		t.Fatalf("listed revoked tokens = %+v, want revoked token", listed.Tokens)
	}

	validateResp = postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "validator-secret", map[string]any{
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
	logOutput := logs.String()
	for _, want := range []string{"control-plane token created", "control-plane tokens listed", "control-plane token revoked"} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs = %q, want %q", logOutput, want)
		}
	}
	if strings.Contains(logOutput, created.Token) {
		t.Fatalf("logs leaked created plaintext token: %q", logOutput)
	}
}

func TestListTokensRequiresAdminAuthorization(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/tokens")
	if err != nil {
		t.Fatalf("GET tokens returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
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

func TestCreateTokenRejectsUnknownScope(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", map[string]any{
		"name":   "agent",
		"scopes": []string{"typo_scope"},
	})
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, `unsupported token scope "typo_scope"`) {
		t.Fatalf("body = %q, want unsupported scope guidance", body)
	}
}

func TestCreateTokenRejectsUnknownJSONFields(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postRaw(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", `{"name":"agent","extra":true}`)
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, `unknown field "extra"`) {
		t.Fatalf("body = %q, want unknown field guidance", body)
	}
}

func TestCreateTokenRejectsTrailingJSON(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postRaw(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", `{"name":"agent"} {"name":"second"}`)
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "multiple json values") {
		t.Fatalf("body = %q, want multiple json guidance", body)
	}
}

func TestCreateTokenRejectsOversizedJSON(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	raw := `{"name":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	resp := postRaw(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", raw)
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "json body is too large") {
		t.Fatalf("body = %q, want body size guidance", body)
	}
}

func TestValidateTokenRequiresValidatorAuthorization(t *testing.T) {
	server := NewServer(Config{ValidatorToken: "validator-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "", map[string]any{
		"token": "ph_test",
		"scope": tokens.ScopeRegisterTunnel,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestValidateTokenRequiresConfiguredValidatorToken(t *testing.T) {
	server := NewServer(Config{}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "validator-secret", map[string]any{
		"token": "ph_test",
		"scope": tokens.ScopeRegisterTunnel,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestValidateTokenRejectsUnknownScope(t *testing.T) {
	server := NewServer(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", map[string]any{
		"name": "agent",
	})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", createResp.StatusCode)
	}
	var created tokens.CreatedToken
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created token returned error: %v", err)
	}

	resp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens/validate", "validator-secret", map[string]any{
		"token": created.Token,
		"scope": "typo_scope",
	})
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, `unsupported token scope "typo_scope"`) {
		t.Fatalf("body = %q, want unsupported scope guidance", body)
	}
}

func TestReservedSubdomainLifecycle(t *testing.T) {
	var logs bytes.Buffer
	server := NewServer(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()), reserved.NewService(reserved.NewMemoryStore()))
	server.logger = slog.New(slog.NewTextHandler(&logs, nil))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createTokenResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", map[string]any{
		"name": "agent",
	})
	defer createTokenResp.Body.Close()
	if createTokenResp.StatusCode != http.StatusCreated {
		t.Fatalf("create token status = %d, want 201", createTokenResp.StatusCode)
	}
	var token tokens.CreatedToken
	if err := json.NewDecoder(createTokenResp.Body).Decode(&token); err != nil {
		t.Fatalf("decode created token returned error: %v", err)
	}

	createResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/reserved-subdomains", "admin-secret", map[string]any{
		"name":     " Demo ",
		"token_id": token.ID,
	})
	defer createResp.Body.Close()
	body := readResponseBody(t, createResp)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create reservation status = %d, want 201; body = %q", createResp.StatusCode, body)
	}
	var created reserved.CreatedReservation
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("decode reservation returned error: %v", err)
	}
	if created.ID == "" || created.Name != "demo" || created.TokenID != token.ID {
		t.Fatalf("created reservation = %+v, want demo owned by token", created)
	}

	duplicateResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/reserved-subdomains", "admin-secret", map[string]any{
		"name":     "demo",
		"token_id": token.ID,
	})
	defer duplicateResp.Body.Close()
	if duplicateResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", duplicateResp.StatusCode)
	}

	listReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/reserved-subdomains", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer admin-secret")
	listResp, err := httpServer.Client().Do(listReq)
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
	var listed reserved.ListReservationsResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode reservation list returned error: %v", err)
	}
	if len(listed.ReservedSubdomains) != 1 || listed.ReservedSubdomains[0].Name != "demo" {
		t.Fatalf("listed reservations = %+v, want demo", listed.ReservedSubdomains)
	}

	authorizeResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/reserved-subdomains/authorize", "validator-secret", map[string]any{
		"token_id":  token.ID,
		"subdomain": "demo",
	})
	defer authorizeResp.Body.Close()
	if authorizeResp.StatusCode != http.StatusOK {
		t.Fatalf("authorize status = %d, want 200", authorizeResp.StatusCode)
	}
	var authorized reserved.AuthorizationResult
	if err := json.NewDecoder(authorizeResp.Body).Decode(&authorized); err != nil {
		t.Fatalf("decode authorization returned error: %v", err)
	}
	if !authorized.Allowed {
		t.Fatalf("authorization = %+v, want allowed", authorized)
	}

	denyResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/reserved-subdomains/authorize", "validator-secret", map[string]any{
		"token_id":  "tok_other",
		"subdomain": "demo",
	})
	defer denyResp.Body.Close()
	if denyResp.StatusCode != http.StatusOK {
		t.Fatalf("deny status = %d, want 200", denyResp.StatusCode)
	}
	var denied reserved.AuthorizationResult
	if err := json.NewDecoder(denyResp.Body).Decode(&denied); err != nil {
		t.Fatalf("decode denied authorization returned error: %v", err)
	}
	if denied.Allowed || denied.Reason != "reserved_for_another_token" {
		t.Fatalf("authorization = %+v, want reserved_for_another_token", denied)
	}

	deleteReq, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, httpServer.URL+"/api/v1/reserved-subdomains/"+created.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	deleteReq.Header.Set("Authorization", "Bearer admin-secret")
	deleteResp, err := httpServer.Client().Do(deleteReq)
	if err != nil {
		t.Fatalf("delete returned error: %v", err)
	}
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleteResp.StatusCode)
	}

	logOutput := logs.String()
	for _, want := range []string{"control-plane reserved subdomain created", "control-plane reserved subdomains listed", "control-plane reserved subdomain deleted"} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs = %q, want %q", logOutput, want)
		}
	}
}

func TestReservedSubdomainCreateValidatesTokenID(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()), reserved.NewService(reserved.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/reserved-subdomains", "admin-secret", map[string]any{
		"name":     "demo",
		"token_id": "tok_missing",
	})
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "token_id was not found") {
		t.Fatalf("body = %q, want token_id guidance", body)
	}
}

func TestReservedSubdomainsRequireAuthorization(t *testing.T) {
	server := NewServer(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()), reserved.NewService(reserved.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/reserved-subdomains")
	if err != nil {
		t.Fatalf("GET reservations returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want 401", resp.StatusCode)
	}

	authorizeResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/reserved-subdomains/authorize", "", map[string]any{
		"token_id":  "tok_owner",
		"subdomain": "demo",
	})
	defer authorizeResp.Body.Close()
	if authorizeResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("authorize status = %d, want 401", authorizeResp.StatusCode)
	}
}

func TestTokenMethodsReturnAllowHeader(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, httpServer.URL+"/api/v1/tokens", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer admin-secret")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); got != "GET, POST" {
		t.Fatalf("Allow = %q, want GET, POST", got)
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

func postRaw(t *testing.T, client *http.Client, url, bearer string, payload string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(payload))
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

func readResponseBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	return string(data)
}

type failingReadyStore struct {
	*tokens.MemoryStore
}

func (s failingReadyStore) Ping(context.Context) error {
	return errors.New("database unavailable")
}
