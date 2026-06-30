// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
