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
	"time"

	"github.com/voiteco/porthook/server/control-plane/internal/access"
	"github.com/voiteco/porthook/server/control-plane/internal/admintokens"
	"github.com/voiteco/porthook/server/control-plane/internal/customdomains"
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

func TestHandlerReturnsRequestIDHeader(t *testing.T) {
	server := NewServer(Config{}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/status", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("X-Correlation-ID", "corr_test")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Request-ID") != "corr_test" {
		t.Fatalf("X-Request-ID = %q, want corr_test", resp.Header.Get("X-Request-ID"))
	}
}

func TestAuditEventsEndpointReturnsRecentEvents(t *testing.T) {
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	unauthorizedResp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/events")
	if err != nil {
		t.Fatalf("GET events returned error: %v", err)
	}
	unauthorizedResp.Body.Close()
	if unauthorizedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorizedResp.StatusCode)
	}

	createResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", map[string]any{
		"name": "agent",
	})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("token create status = %d, want 201", createResp.StatusCode)
	}
	var created tokens.CreatedToken
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode token returned error: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/events?limit=10", nil)
	if err != nil {
		t.Fatalf("NewRequest events returned error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer admin-secret")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("GET events authorized returned error: %v", err)
	}
	defer resp.Body.Close()
	body := readResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d, want 200; body = %q", resp.StatusCode, body)
	}
	if strings.Contains(body, created.Token) {
		t.Fatalf("events leaked plaintext token: %q", body)
	}
	var events auditEventsResponse
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("decode events returned error: %v", err)
	}
	if len(events.Events) == 0 {
		t.Fatal("events response is empty")
	}
	var foundTokenCreated bool
	var foundAuthFailed bool
	for _, event := range events.Events {
		if event.Event == "control_plane.token_created" {
			foundTokenCreated = true
			if event.Fields["token_id"] == "" || event.Fields["name"] != "agent" {
				t.Fatalf("token_created event = %+v, want token fields", event)
			}
		}
		if event.Event == "control_plane.auth_failed" {
			foundAuthFailed = true
		}
	}
	if !foundTokenCreated || !foundAuthFailed {
		t.Fatalf("events = %+v, want token_created and auth_failed", events.Events)
	}
}

func TestAuditEventsEndpointFiltersAndPaginates(t *testing.T) {
	auditStore := NewMemoryAuditEventStore(10)
	server := NewServerWithAuditEvents(
		Config{AdminToken: "admin-secret"},
		tokens.NewService(tokens.NewMemoryStore()),
		nil,
		nil,
		nil,
		auditStore,
	)
	base := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	for _, event := range []AuditEvent{
		{
			Time:   base,
			Level:  "INFO",
			Event:  "control_plane.token_created",
			Fields: map[string]string{"token_id": "tok_keep", "name": "agent"},
		},
		{
			Time:   base.Add(time.Second),
			Level:  "WARN",
			Event:  "control_plane.auth_failed",
			Fields: map[string]string{"surface": "admin"},
		},
		{
			Time:   base.Add(2 * time.Second),
			Level:  "INFO",
			Event:  "control_plane.token_deleted",
			Fields: map[string]string{"token_id": "tok_keep"},
		},
	} {
		if err := auditStore.Add(context.Background(), event); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/events?limit=1&event=token&field=tok_keep", nil)
	if err != nil {
		t.Fatalf("NewRequest events returned error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer admin-secret")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("GET events returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d, want 200; body = %q", resp.StatusCode, readResponseBody(t, resp))
	}
	var first auditEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatalf("decode events returned error: %v", err)
	}
	if len(first.Events) != 1 || first.Events[0].Event != "control_plane.token_deleted" {
		t.Fatalf("first events = %+v, want newest matching token event", first.Events)
	}
	if first.NextCursor == "" {
		t.Fatal("next cursor is empty, want second page cursor")
	}
	if first.Filters.Limit != 1 || first.Filters.Event != "token" || first.Filters.Field != "tok_keep" {
		t.Fatalf("filters = %+v, want echoed limit/event/field filters", first.Filters)
	}

	nextReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/events?limit=1&event=token&field=tok_keep&cursor="+first.NextCursor, nil)
	if err != nil {
		t.Fatalf("NewRequest second page returned error: %v", err)
	}
	nextReq.Header.Set("Authorization", "Bearer admin-secret")
	nextResp, err := httpServer.Client().Do(nextReq)
	if err != nil {
		t.Fatalf("GET second page returned error: %v", err)
	}
	defer nextResp.Body.Close()
	if nextResp.StatusCode != http.StatusOK {
		t.Fatalf("second page status = %d, want 200; body = %q", nextResp.StatusCode, readResponseBody(t, nextResp))
	}
	var second auditEventsResponse
	if err := json.NewDecoder(nextResp.Body).Decode(&second); err != nil {
		t.Fatalf("decode second page returned error: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].Event != "control_plane.token_created" {
		t.Fatalf("second events = %+v, want older matching token event", second.Events)
	}
	if second.Filters.Cursor != first.NextCursor {
		t.Fatalf("cursor filter = %q, want %q", second.Filters.Cursor, first.NextCursor)
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
	if !strings.Contains(body, "Admin tokens") || !strings.Contains(body, "Reserved subdomains") || !strings.Contains(body, "Custom domains") || !strings.Contains(body, "Access policies") || !strings.Contains(body, "Operational overview") || !strings.Contains(body, "Active tunnels") || !strings.Contains(body, "Request logs") {
		t.Fatalf("body = %q, want dashboard admin tokens, reserved subdomains, custom domains, access policies, operational overview, active tunnels, and request logs UI", body)
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
	if !strings.Contains(body, "/api/v1/admin-tokens") || !strings.Contains(body, "/api/v1/reserved-subdomains") || !strings.Contains(body, "/api/v1/custom-domains") || !strings.Contains(body, "/api/v1/access-policies") || !strings.Contains(body, "/api/v1/tunnels") || !strings.Contains(body, "/api/v1/request-logs") {
		t.Fatalf("asset body = %q, want admin token, reservation, custom domain, access policy, tunnel, and request logs API clients", body)
	}
	if !strings.Contains(body, "renderOperationalOverview") {
		t.Fatalf("asset body = %q, want operational overview renderer", body)
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

func TestReadyzReportsAuditStoreFailure(t *testing.T) {
	server := NewServerWithAuditEvents(
		Config{},
		tokens.NewService(tokens.NewMemoryStore()),
		reserved.NewService(reserved.NewMemoryStore()),
		access.NewService(access.NewMemoryStore()),
		customdomains.NewService(customdomains.NewMemoryStore()),
		failingAuditEventStore{},
	)
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
	if !strings.Contains(body, "audit store unavailable") {
		t.Fatalf("body = %q, want audit store error", body)
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
		"porthook_control_plane_ready 1",
		"porthook_control_plane_uptime_seconds ",
		"porthook_control_plane_tokens 1",
		"porthook_control_plane_tokens_revoked 0",
		"porthook_control_plane_reserved_subdomains 0",
		"porthook_control_plane_custom_domains 0",
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
	for _, want := range []string{
		"control-plane token created",
		"control-plane tokens listed",
		"control-plane token revoked",
		"event=control_plane.token_created",
		"event=control_plane.tokens_listed",
		"event=control_plane.token_validated",
		"event=control_plane.token_revoked",
		"token_id=" + created.ID,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs = %q, want %q", logOutput, want)
		}
	}
	if strings.Contains(logOutput, created.Token) {
		t.Fatalf("logs leaked created plaintext token: %q", logOutput)
	}
	for _, secret := range []string{"admin-secret", "validator-secret"} {
		if strings.Contains(logOutput, secret) {
			t.Fatalf("logs leaked configured secret %q: %q", secret, logOutput)
		}
	}
}

func TestListTokensRequiresAdminAuthorization(t *testing.T) {
	var logs bytes.Buffer
	server := NewServer(Config{AdminToken: "admin-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	server.logger = slog.New(slog.NewTextHandler(&logs, nil))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/tokens", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer wrong-admin-secret")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("GET tokens returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, "event=control_plane.auth_failed") || !strings.Contains(logOutput, "surface=admin") {
		t.Fatalf("logs = %q, want admin auth failure audit event", logOutput)
	}
	if strings.Contains(logOutput, "wrong-admin-secret") || strings.Contains(logOutput, "admin-secret") {
		t.Fatalf("logs leaked admin token material: %q", logOutput)
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

func TestAdminTokenEndpointsCreateListAndRevokeScopedTokens(t *testing.T) {
	server := NewServer(Config{AdminToken: "bootstrap-secret"}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/admin-tokens", "bootstrap-secret", map[string]any{
		"name":   "history reader",
		"scopes": []string{admintokens.ScopeAuditHistory},
	})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%q", createResp.StatusCode, readResponseBody(t, createResp))
	}
	var created admintokens.CreatedToken
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created admin token returned error: %v", err)
	}
	if created.Token == "" || !strings.HasPrefix(created.Token, "pat_") {
		t.Fatalf("created token = %q, want pat_ plaintext token", created.Token)
	}

	listResp := getWithBearer(t, httpServer.Client(), httpServer.URL+"/api/v1/admin-tokens", "bootstrap-secret")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
	body := readResponseBody(t, listResp)
	if !strings.Contains(body, created.ID) {
		t.Fatalf("list body = %q, want created admin token id", body)
	}
	if strings.Contains(body, created.Token) {
		t.Fatalf("list body leaked plaintext admin token: %q", body)
	}

	eventsResp := getWithBearer(t, httpServer.Client(), httpServer.URL+"/api/v1/events?limit=1", created.Token)
	defer eventsResp.Body.Close()
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d, want 200", eventsResp.StatusCode)
	}

	tokensResp := getWithBearer(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", created.Token)
	defer tokensResp.Body.Close()
	if tokensResp.StatusCode != http.StatusForbidden {
		t.Fatalf("tokens status = %d, want 403", tokensResp.StatusCode)
	}

	revokeReq, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, httpServer.URL+"/api/v1/admin-tokens/"+created.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	revokeReq.Header.Set("Authorization", "Bearer bootstrap-secret")
	revokeResp, err := httpServer.Client().Do(revokeReq)
	if err != nil {
		t.Fatalf("Do revoke returned error: %v", err)
	}
	revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", revokeResp.StatusCode)
	}

	revokedResp := getWithBearer(t, httpServer.Client(), httpServer.URL+"/api/v1/events?limit=1", created.Token)
	defer revokedResp.Body.Close()
	if revokedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d, want 401", revokedResp.StatusCode)
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
	for _, want := range []string{
		"control-plane reserved subdomain created",
		"control-plane reserved subdomains listed",
		"control-plane reserved subdomain deleted",
		"event=control_plane.reservation_created",
		"event=control_plane.reservations_listed",
		"event=control_plane.reservation_authorized",
		"event=control_plane.reservation_deleted",
		"token_id=" + token.ID,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs = %q, want %q", logOutput, want)
		}
	}
	if strings.Contains(logOutput, token.Token) {
		t.Fatalf("logs leaked plaintext token: %q", logOutput)
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

func TestAccessPolicyLifecycleAndEvaluation(t *testing.T) {
	ctx := context.Background()
	tokenService := tokens.NewService(tokens.NewMemoryStore())
	reservationService := reserved.NewService(reserved.NewMemoryStore())
	accessPolicyService := access.NewService(access.NewMemoryStore())
	token, err := tokenService.CreateToken(ctx, tokens.CreateTokenRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	reservation, err := reservationService.CreateReservation(ctx, reserved.CreateReservationRequest{
		Name:    "demo",
		TokenID: token.ID,
	})
	if err != nil {
		t.Fatalf("CreateReservation returned error: %v", err)
	}

	var logs bytes.Buffer
	server := NewServerWithAccessPolicies(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokenService, reservationService, accessPolicyService)
	server.logger = slog.New(slog.NewTextHandler(&logs, nil))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/access-policies", "admin-secret", map[string]any{
		"reserved_subdomain_id": reservation.ID,
		"mode":                  "basic_auth",
		"basic_username":        "admin",
		"basic_password":        "secret",
	})
	defer createResp.Body.Close()
	createBody := readResponseBody(t, createResp)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body = %q", createResp.StatusCode, createBody)
	}
	if strings.Contains(createBody, "basic_password") || strings.Contains(createBody, `"secret"`) {
		t.Fatalf("create response leaked secret: %q", createBody)
	}
	var created access.PolicySummary
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("decode created policy returned error: %v", err)
	}
	if created.ID == "" || created.Mode != access.ModeBasicAuth || !created.SecretConfigured {
		t.Fatalf("created policy = %+v, want basic_auth with configured secret", created)
	}

	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/v1/access-policies", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	listReq.Header.Set("Authorization", "Bearer admin-secret")
	listResp, err := httpServer.Client().Do(listReq)
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	defer listResp.Body.Close()
	listBody := readResponseBody(t, listResp)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %q", listResp.StatusCode, listBody)
	}
	if strings.Contains(listBody, "basic_password") || strings.Contains(listBody, `"secret"`) {
		t.Fatalf("list response leaked secret: %q", listBody)
	}

	deniedResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/access-policies/evaluate", "validator-secret", map[string]any{
		"subdomain":      "demo",
		"basic_username": "admin",
		"basic_password": "wrong",
	})
	defer deniedResp.Body.Close()
	if deniedResp.StatusCode != http.StatusOK {
		t.Fatalf("denied status = %d, want 200", deniedResp.StatusCode)
	}
	var denied evaluateAccessPolicyResponse
	if err := json.NewDecoder(deniedResp.Body).Decode(&denied); err != nil {
		t.Fatalf("decode denied returned error: %v", err)
	}
	if denied.Allowed || denied.Mode != access.ModeBasicAuth || denied.Reason != "basic_auth_required" {
		t.Fatalf("denied = %+v, want basic_auth_required", denied)
	}

	allowedResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/access-policies/evaluate", "validator-secret", map[string]any{
		"subdomain":      "demo",
		"basic_username": "admin",
		"basic_password": "secret",
	})
	defer allowedResp.Body.Close()
	if allowedResp.StatusCode != http.StatusOK {
		t.Fatalf("allowed status = %d, want 200", allowedResp.StatusCode)
	}
	var allowed evaluateAccessPolicyResponse
	if err := json.NewDecoder(allowedResp.Body).Decode(&allowed); err != nil {
		t.Fatalf("decode allowed returned error: %v", err)
	}
	if !allowed.Allowed || allowed.Mode != access.ModeBasicAuth {
		t.Fatalf("allowed = %+v, want basic_auth allow", allowed)
	}

	updatePayload := strings.NewReader(`{"mode":"public"}`)
	updateReq, err := http.NewRequestWithContext(ctx, http.MethodPut, httpServer.URL+"/api/v1/access-policies/"+created.ID, updatePayload)
	if err != nil {
		t.Fatalf("NewRequest update returned error: %v", err)
	}
	updateReq.Header.Set("Authorization", "Bearer admin-secret")
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp, err := httpServer.Client().Do(updateReq)
	if err != nil {
		t.Fatalf("update returned error: %v", err)
	}
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body = %q", updateResp.StatusCode, readResponseBody(t, updateResp))
	}

	deleteReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, httpServer.URL+"/api/v1/access-policies/"+created.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest delete returned error: %v", err)
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
	for _, want := range []string{
		"event=control_plane.access_policy_created",
		"event=control_plane.access_policies_listed",
		"event=control_plane.access_policy_evaluated",
		"event=control_plane.access_policy_updated",
		"event=control_plane.access_policy_deleted",
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs = %q, want %q", logOutput, want)
		}
	}
	if strings.Contains(logOutput, "basic_password") || strings.Contains(logOutput, `"secret"`) {
		t.Fatalf("logs leaked secret: %q", logOutput)
	}
}

func TestCustomDomainLifecycle(t *testing.T) {
	var logs bytes.Buffer
	txtResolver := testTXTResolver{values: map[string][]string{}}
	server := NewServerWithCustomDomains(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()), nil, nil, customdomains.NewServiceWithResolver(customdomains.NewMemoryStore(), txtResolver))
	server.logger = slog.New(slog.NewTextHandler(&logs, nil))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createTokenResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/tokens", "admin-secret", map[string]any{
		"name": "agent",
	})
	defer createTokenResp.Body.Close()
	if createTokenResp.StatusCode != http.StatusCreated {
		t.Fatalf("token create status = %d, want 201", createTokenResp.StatusCode)
	}
	var createdToken tokens.CreatedToken
	if err := json.NewDecoder(createTokenResp.Body).Decode(&createdToken); err != nil {
		t.Fatalf("decode token returned error: %v", err)
	}

	createReservationResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/reserved-subdomains", "admin-secret", map[string]any{
		"name":     "demo",
		"token_id": createdToken.ID,
	})
	defer createReservationResp.Body.Close()
	if createReservationResp.StatusCode != http.StatusCreated {
		t.Fatalf("reservation create status = %d, want 201", createReservationResp.StatusCode)
	}
	var createdReservation reserved.CreatedReservation
	if err := json.NewDecoder(createReservationResp.Body).Decode(&createdReservation); err != nil {
		t.Fatalf("decode reservation returned error: %v", err)
	}

	createDomainResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/custom-domains", "admin-secret", map[string]any{
		"hostname":              "Preview.Example.Test.",
		"reserved_subdomain_id": createdReservation.ID,
	})
	defer createDomainResp.Body.Close()
	if createDomainResp.StatusCode != http.StatusCreated {
		body := readResponseBody(t, createDomainResp)
		t.Fatalf("custom domain create status = %d, want 201; body = %q", createDomainResp.StatusCode, body)
	}
	var createdDomain customdomains.CreatedDomain
	if err := json.NewDecoder(createDomainResp.Body).Decode(&createdDomain); err != nil {
		t.Fatalf("decode custom domain returned error: %v", err)
	}
	if createdDomain.Hostname != "preview.example.test" || createdDomain.ReservedSubdomainID != createdReservation.ID || createdDomain.Status != customdomains.StatusPendingVerification {
		t.Fatalf("created custom domain = %+v, want normalized pending mapping", createdDomain)
	}
	if createdDomain.VerificationToken == "" || createdDomain.VerificationName != "_porthook.preview.example.test" {
		t.Fatalf("created custom domain verification = %+v, want token and verification name", createdDomain)
	}

	listReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/custom-domains", nil)
	if err != nil {
		t.Fatalf("NewRequest list returned error: %v", err)
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
	var listed customdomains.ListDomainsResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list returned error: %v", err)
	}
	if len(listed.CustomDomains) != 1 || listed.CustomDomains[0].ID != createdDomain.ID {
		t.Fatalf("listed custom domains = %+v, want created domain", listed.CustomDomains)
	}

	getReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/custom-domains/"+createdDomain.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest get returned error: %v", err)
	}
	getReq.Header.Set("Authorization", "Bearer admin-secret")
	getResp, err := httpServer.Client().Do(getReq)
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d, want 200", getResp.StatusCode)
	}

	pendingLookupResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/custom-domains/lookup", "validator-secret", map[string]any{
		"hostname": "preview.example.test",
	})
	defer pendingLookupResp.Body.Close()
	if pendingLookupResp.StatusCode != http.StatusOK {
		body := readResponseBody(t, pendingLookupResp)
		t.Fatalf("pending lookup status = %d, want 200; body = %q", pendingLookupResp.StatusCode, body)
	}
	var pendingLookup lookupCustomDomainResponse
	if err := json.NewDecoder(pendingLookupResp.Body).Decode(&pendingLookup); err != nil {
		t.Fatalf("decode pending lookup returned error: %v", err)
	}
	if pendingLookup.Found || pendingLookup.Reason != "not_verified" || pendingLookup.CustomDomainID != createdDomain.ID {
		t.Fatalf("pending lookup = %+v, want not_verified custom domain", pendingLookup)
	}

	txtResolver.values[createdDomain.VerificationName] = []string{"porthook-domain-verification=" + createdDomain.VerificationToken}
	verifyResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/custom-domains/"+createdDomain.ID+"/verify", "admin-secret", nil)
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		body := readResponseBody(t, verifyResp)
		t.Fatalf("verify status = %d, want 200; body = %q", verifyResp.StatusCode, body)
	}
	var verified customdomains.VerifyDomainResponse
	if err := json.NewDecoder(verifyResp.Body).Decode(&verified); err != nil {
		t.Fatalf("decode verify returned error: %v", err)
	}
	if verified.Status != customdomains.StatusActive || verified.VerifiedAt == nil {
		t.Fatalf("verified custom domain = %+v, want active with verified_at", verified)
	}

	lookupResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/custom-domains/lookup", "validator-secret", map[string]any{
		"hostname": "preview.example.test",
	})
	defer lookupResp.Body.Close()
	if lookupResp.StatusCode != http.StatusOK {
		body := readResponseBody(t, lookupResp)
		t.Fatalf("lookup status = %d, want 200; body = %q", lookupResp.StatusCode, body)
	}
	var lookup lookupCustomDomainResponse
	if err := json.NewDecoder(lookupResp.Body).Decode(&lookup); err != nil {
		t.Fatalf("decode lookup returned error: %v", err)
	}
	if !lookup.Found || lookup.Subdomain != "demo" || lookup.CustomDomainID != createdDomain.ID {
		t.Fatalf("lookup = %+v, want found demo custom domain", lookup)
	}

	missingLookupResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/custom-domains/lookup", "validator-secret", map[string]any{
		"hostname": "missing.example.test",
	})
	defer missingLookupResp.Body.Close()
	if missingLookupResp.StatusCode != http.StatusOK {
		t.Fatalf("missing lookup status = %d, want 200", missingLookupResp.StatusCode)
	}
	var missing lookupCustomDomainResponse
	if err := json.NewDecoder(missingLookupResp.Body).Decode(&missing); err != nil {
		t.Fatalf("decode missing lookup returned error: %v", err)
	}
	if missing.Found || missing.Reason != "not_found" {
		t.Fatalf("missing lookup = %+v, want not_found", missing)
	}

	deleteReq, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, httpServer.URL+"/api/v1/custom-domains/"+createdDomain.ID, nil)
	if err != nil {
		t.Fatalf("NewRequest delete returned error: %v", err)
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

	for _, want := range []string{"control_plane.custom_domain_created", "hostname=preview.example.test", "control_plane.custom_domain_verification_checked", "control_plane.custom_domain_lookup", "control_plane.custom_domain_deleted"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs = %q, want %q", logs.String(), want)
		}
	}
}

func TestCustomDomainEndpointsRequireAuthorization(t *testing.T) {
	server := NewServer(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/custom-domains")
	if err != nil {
		t.Fatalf("GET custom domains returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want 401", resp.StatusCode)
	}

	lookupResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/custom-domains/lookup", "", map[string]any{
		"hostname": "preview.example.test",
	})
	defer lookupResp.Body.Close()
	if lookupResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("lookup status = %d, want 401", lookupResp.StatusCode)
	}
}

func TestAccessPolicyEndpointsRequireAuthorization(t *testing.T) {
	server := NewServer(Config{
		AdminToken:     "admin-secret",
		ValidatorToken: "validator-secret",
	}, tokens.NewService(tokens.NewMemoryStore()))
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/access-policies")
	if err != nil {
		t.Fatalf("GET access policies returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want 401", resp.StatusCode)
	}

	evaluateResp := postJSON(t, httpServer.Client(), httpServer.URL+"/api/v1/access-policies/evaluate", "", map[string]any{
		"subdomain": "demo",
	})
	defer evaluateResp.Body.Close()
	if evaluateResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("evaluate status = %d, want 401", evaluateResp.StatusCode)
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

func getWithBearer(t *testing.T, client *http.Client, url, bearer string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
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

type testTXTResolver struct {
	values map[string][]string
	err    error
}

func (r testTXTResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.values[name], nil
}

type failingReadyStore struct {
	*tokens.MemoryStore
}

func (s failingReadyStore) Ping(context.Context) error {
	return errors.New("database unavailable")
}

type failingAuditEventStore struct{}

func (s failingAuditEventStore) Ping(context.Context) error {
	return errors.New("audit store unavailable")
}

func (s failingAuditEventStore) Add(context.Context, AuditEvent) error {
	return nil
}

func (s failingAuditEventStore) List(context.Context, AuditEventListOptions) (AuditEventListPage, error) {
	return AuditEventListPage{}, errors.New("audit store unavailable")
}
