// SPDX-License-Identifier: AGPL-3.0-only

package gateway

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
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wswire"
	"github.com/voiteco/porthook/server/gateway/internal/registry"
)

func TestAgentWebSocketAuthAndRegister(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "dev-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthOK {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthOK)
	}
	authOKPayload, err := messages.DecodePayload[messages.AuthOK](authResp)
	if err != nil {
		t.Fatalf("decode auth ok returned error: %v", err)
	}
	if authOKPayload.ProtocolVersion != messages.ProtocolVersion {
		t.Fatalf("auth protocol version = %q, want %q", authOKPayload.ProtocolVersion, messages.ProtocolVersion)
	}
	if len(authOKPayload.Capabilities) != len(messages.DefaultProtocolCapabilities()) {
		t.Fatalf("auth capabilities count = %d, want %d", len(authOKPayload.Capabilities), len(messages.DefaultProtocolCapabilities()))
	}

	reg, err := messages.New(messages.TypeTunnelRegister, messages.TunnelRegister{
		Protocol:           "http",
		LocalTarget:        "http://localhost:3000",
		RequestedSubdomain: "demo",
	})
	if err != nil {
		t.Fatalf("New registration returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, reg); err != nil {
		t.Fatalf("write registration returned error: %v", err)
	}

	var regResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &regResp); err != nil {
		t.Fatalf("read registration response returned error: %v", err)
	}
	if regResp.Type != messages.TypeTunnelRegistered {
		t.Fatalf("registration response type = %s, want %s", regResp.Type, messages.TypeTunnelRegistered)
	}

	payload, err := messages.DecodePayload[messages.TunnelRegistered](regResp)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	if payload.Subdomain != "demo" {
		t.Fatalf("subdomain = %q, want demo", payload.Subdomain)
	}
	if payload.TunnelID == "" {
		t.Fatal("tunnel id is empty")
	}
	if payload.PublicURL != "http://demo.localhost:8080" {
		t.Fatalf("public url = %q, want http://demo.localhost:8080", payload.PublicURL)
	}
}

func TestAgentWebSocketRejectsUnsupportedProtocol(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "dev-token",
		ProtocolVersion: "0.1",
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthError {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthError)
	}

	payload, err := messages.DecodePayload[messages.ErrorPayload](authResp)
	if err != nil {
		t.Fatalf("decode auth error returned error: %v", err)
	}
	if payload.Code != "unsupported_protocol" {
		t.Fatalf("auth error code = %q, want unsupported_protocol", payload.Code)
	}
	if !strings.Contains(payload.Message, "protocol version") {
		t.Fatalf("auth error message = %q, want protocol version mismatch", payload.Message)
	}
}

func TestAgentWebSocketRejectsMissingCapabilities(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "dev-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    []string{messages.CapabilityStreamStartEnd},
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthError {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthError)
	}

	payload, err := messages.DecodePayload[messages.ErrorPayload](authResp)
	if err != nil {
		t.Fatalf("decode auth error returned error: %v", err)
	}
	if payload.Code != "unsupported_protocol" {
		t.Fatalf("auth error code = %q, want unsupported_protocol", payload.Code)
	}
	if !strings.Contains(payload.Message, messages.CapabilityBinaryBodyFrame) {
		t.Fatalf("auth error message = %q, want binary_body_frames", payload.Message)
	}
}

func TestAgentWebSocketValidatesTokenThroughControlPlane(t *testing.T) {
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tokens/validate" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer validator-secret" {
			t.Errorf("authorization = %q, want validator bearer", r.Header.Get("Authorization"))
		}
		var req struct {
			Token string `json:"token"`
			Scope string `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Decode returned error: %v", err)
			return
		}
		if req.Token != "control-token" {
			t.Errorf("token = %q, want control-token", req.Token)
		}
		if req.Scope != "register_tunnel" {
			t.Errorf("scope = %q, want register_tunnel", req.Scope)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":    true,
			"token_id": "tok_test",
			"scopes":   []string{"register_tunnel"},
		})
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.StaticToken = "static-token"
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	server := NewServer(cfg, slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "control-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthOK {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthOK)
	}
}

func TestAgentWebSocketAuthorizesRequestedSubdomainThroughControlPlane(t *testing.T) {
	authorizeCalled := false
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer validator-secret" {
			t.Errorf("authorization = %q, want validator bearer", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v1/tokens/validate":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"valid":    true,
				"token_id": "tok_owner",
				"scopes":   []string{"register_tunnel"},
			})
		case "/api/v1/reserved-subdomains/authorize":
			authorizeCalled = true
			var req struct {
				TokenID   string `json:"token_id"`
				Subdomain string `json:"subdomain"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("Decode returned error: %v", err)
				return
			}
			if req.TokenID != "tok_owner" || req.Subdomain != "demo" {
				t.Errorf("authorization request = %+v, want tok_owner/demo", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"allowed": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.StaticToken = "static-token"
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	server := NewServer(cfg, slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "control-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}
	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthOK {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthOK)
	}

	writeRegistrationForTest(t, ctx, conn, "demo")
	var regResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &regResp); err != nil {
		t.Fatalf("read registration response returned error: %v", err)
	}
	if regResp.Type != messages.TypeTunnelRegistered {
		t.Fatalf("registration response type = %s, want %s", regResp.Type, messages.TypeTunnelRegistered)
	}
	if !authorizeCalled {
		t.Fatal("control-plane reserved subdomain authorization was not called")
	}
}

func TestAgentWebSocketRejectsUnreservedRequestedSubdomain(t *testing.T) {
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tokens/validate":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"valid":    true,
				"token_id": "tok_owner",
				"scopes":   []string{"register_tunnel"},
			})
		case "/api/v1/reserved-subdomains/authorize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"allowed": false,
				"reason":  "not_reserved",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.StaticToken = "static-token"
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	server := NewServer(cfg, slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "control-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}
	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthOK {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthOK)
	}

	writeRegistrationForTest(t, ctx, conn, "demo")
	var resp messages.Envelope
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read registration error returned error: %v", err)
	}
	if resp.Type != messages.TypeTunnelError {
		t.Fatalf("registration response type = %s, want %s", resp.Type, messages.TypeTunnelError)
	}
	payload, err := messages.DecodePayload[messages.ErrorPayload](resp)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	if payload.Code != "subdomain_not_reserved" {
		t.Fatalf("error code = %q, want subdomain_not_reserved", payload.Code)
	}
	if !strings.Contains(payload.Message, `subdomain "demo" is not reserved`) {
		t.Fatalf("error message = %q, want reservation guidance", payload.Message)
	}
}

func TestAgentWebSocketRejectsInvalidControlPlaneToken(t *testing.T) {
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": false})
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.StaticToken = "static-token"
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	server := NewServer(cfg, slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "wrong",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthError {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthError)
	}
	payload, err := messages.DecodePayload[messages.ErrorPayload](authResp)
	if err != nil {
		t.Fatalf("decode auth error returned error: %v", err)
	}
	if payload.Code != "invalid_token" {
		t.Fatalf("auth error code = %q, want invalid_token", payload.Code)
	}
}

func TestAgentWebSocketFailsClosedForInvalidControlPlaneURL(t *testing.T) {
	cfg := testConfig()
	cfg.StaticToken = "dev-token"
	cfg.ControlPlaneURL = "://bad-url"
	cfg.ControlPlaneToken = "validator-secret"
	server := NewServer(cfg, slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "dev-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthError {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthError)
	}
	payload, err := messages.DecodePayload[messages.ErrorPayload](authResp)
	if err != nil {
		t.Fatalf("decode auth error returned error: %v", err)
	}
	if payload.Code != "auth_unavailable" {
		t.Fatalf("auth error code = %q, want auth_unavailable", payload.Code)
	}
}

func TestAgentWebSocketFailsClosedWhenControlPlaneTokenMissing(t *testing.T) {
	cfg := testConfig()
	cfg.StaticToken = "dev-token"
	cfg.ControlPlaneURL = "http://control.example"
	server := NewServer(cfg, slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "dev-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthError {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthError)
	}
	payload, err := messages.DecodePayload[messages.ErrorPayload](authResp)
	if err != nil {
		t.Fatalf("decode auth error returned error: %v", err)
	}
	if payload.Code != "auth_unavailable" {
		t.Fatalf("auth error code = %q, want auth_unavailable", payload.Code)
	}
}

func TestAgentWebSocketRejectsDuplicateRequestedSubdomain(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	firstConn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("first Dial returned error: %v", err)
	}
	defer firstConn.Close(websocket.StatusNormalClosure, "")
	registerAgent(t, ctx, firstConn, "demo")

	secondConn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("second Dial returned error: %v", err)
	}
	defer secondConn.Close(websocket.StatusNormalClosure, "")

	authenticateAgentForTest(t, ctx, secondConn)
	writeRegistrationForTest(t, ctx, secondConn, "demo")

	var resp messages.Envelope
	if err := wsjson.Read(ctx, secondConn, &resp); err != nil {
		t.Fatalf("read registration error returned error: %v", err)
	}
	if resp.Type != messages.TypeTunnelError {
		t.Fatalf("registration response type = %s, want %s", resp.Type, messages.TypeTunnelError)
	}

	payload, err := messages.DecodePayload[messages.ErrorPayload](resp)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	if payload.Code != "subdomain_unavailable" {
		t.Fatalf("error code = %q, want subdomain_unavailable", payload.Code)
	}
	if !strings.Contains(payload.Message, `subdomain "demo" is already in use`) {
		t.Fatalf("error message = %q, want duplicate subdomain guidance", payload.Message)
	}
}

func TestAgentWebSocketReleasesSubdomainAfterDisconnect(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	firstConn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("first Dial returned error: %v", err)
	}
	registerAgent(t, ctx, firstConn, "demo")
	_ = firstConn.Close(websocket.StatusGoingAway, "test disconnect")

	waitForPublicStatus(t, ctx, publicServer.Client(), publicServer.URL+"/after-disconnect", "demo.localhost", http.StatusNotFound)

	secondConn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("second Dial returned error: %v", err)
	}
	defer secondConn.Close(websocket.StatusNormalClosure, "")

	registered := registerAgentAndRead(t, ctx, secondConn, "demo")
	if registered.Subdomain != "demo" {
		t.Fatalf("subdomain = %q, want demo", registered.Subdomain)
	}
}

func TestAgentWebSocketRetriesRandomSubdomainCollision(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	subdomains := []string{"taken", "available"}
	server.generateSubdomain = func(_ int) (string, error) {
		if len(subdomains) == 0 {
			t.Fatal("generateSubdomain called too many times")
		}
		next := subdomains[0]
		subdomains = subdomains[1:]
		return next, nil
	}

	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	firstConn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("first Dial returned error: %v", err)
	}
	defer firstConn.Close(websocket.StatusNormalClosure, "")
	registerAgent(t, ctx, firstConn, "taken")

	secondConn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("second Dial returned error: %v", err)
	}
	defer secondConn.Close(websocket.StatusNormalClosure, "")

	registered := registerAgentAndRead(t, ctx, secondConn, "")
	if registered.Subdomain != "available" {
		t.Fatalf("subdomain = %q, want available", registered.Subdomain)
	}
	if len(subdomains) != 0 {
		t.Fatalf("unused random subdomains = %v, want none", subdomains)
	}
}

func TestPublicRequestRoundTrip(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	agentErr := make(chan error, 1)
	go func() {
		reqEnv, payload, body, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}

		if payload.Method != http.MethodPost {
			agentErr <- errString("method was not forwarded")
			return
		}
		if payload.Path != "/hello" || payload.Query != "x=1" {
			agentErr <- errString("path or query was not forwarded")
			return
		}
		if payload.Header.Get("Connection") != "" {
			agentErr <- errString("hop-by-hop request header was forwarded")
			return
		}
		if payload.Header.Get("X-Porthook-Tunnel-ID") == "" {
			agentErr <- errString("tunnel header was not added")
			return
		}
		if string(body) != "payload" {
			agentErr <- errString("request body was not forwarded")
			return
		}

		resp := httpwire.Response{
			Status: http.StatusAccepted,
			Header: http.Header{
				"Content-Type": []string{"text/plain"},
				"Connection":   []string{"close"},
			},
			Body: []byte("via-agent"),
		}
		if err := writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, resp); err != nil {
			agentErr <- err
			return
		}
		agentErr <- nil
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, publicServer.URL+"/hello?x=1", bytes.NewBufferString("payload"))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"
	req.Header.Set("Connection", "close")

	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Connection") != "" {
		t.Fatal("hop-by-hop response header was forwarded")
	}
	if string(body) != "via-agent" {
		t.Fatalf("body = %q, want via-agent", string(body))
	}

	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestPublicRequestStreamsBodyAcrossChunks(t *testing.T) {
	cfg := testConfig()
	cfg.StreamChunkBytes = 4
	cfg.MaxBodyBytes = 64
	server := NewServer(cfg, slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	wantBody := strings.Repeat("abcd", 5) + "tail"
	agentErr := make(chan error, 1)
	go func() {
		reqEnv, payload, body, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		if payload.ContentLength != int64(len(wantBody)) {
			agentErr <- errString("content length was not forwarded")
			return
		}
		if string(body) != wantBody {
			agentErr <- errString("chunked request body was not forwarded")
			return
		}

		agentErr <- writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("ok"),
		})
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, publicServer.URL+"/upload", strings.NewReader(wantBody))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"

	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestPublicRequestRejectsLargeBody(t *testing.T) {
	cfg := testConfig()
	cfg.MaxBodyBytes = 3
	server := NewServer(cfg, slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, publicServer.URL+"/upload", bytes.NewBufferString("too-large"))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"

	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestPublicRequestMapsStreamErrorToBadGateway(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	agentErr := make(chan error, 1)
	go func() {
		reqEnv, _, _, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		streamErr, err := messages.NewStream(messages.TypeHTTPStreamError, reqEnv.StreamID, reqEnv.TunnelID, messages.ErrorPayload{
			Code:    "local_request_failed",
			Message: "local service failed",
		})
		if err != nil {
			agentErr <- err
			return
		}
		agentErr <- wsjson.Write(ctx, conn, streamErr)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/broken", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"

	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestPublicRequestTimesOutWaitingForAgent(t *testing.T) {
	cfg := testConfig()
	cfg.StreamTimeout = 50 * time.Millisecond
	server := NewServer(cfg, slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	agentErr := make(chan error, 1)
	go func() {
		if _, _, _, err := readStreamedRequest(ctx, conn); err != nil {
			agentErr <- err
			return
		}
		agentErr <- nil
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/slow", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"

	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", resp.StatusCode)
	}

	select {
	case err := <-agentErr:
		if err != nil {
			t.Fatalf("agent goroutine returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("agent did not receive timed-out request")
	}
}

func TestPublicRequestTimeoutSendsStreamCancel(t *testing.T) {
	cfg := testConfig()
	cfg.StreamTimeout = 50 * time.Millisecond
	server := NewServer(cfg, slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	agentErr := make(chan error, 1)
	go func() {
		reqEnv, _, _, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}

		var cancelEnv messages.Envelope
		if err := wsjson.Read(ctx, conn, &cancelEnv); err != nil {
			agentErr <- err
			return
		}
		if cancelEnv.Type != messages.TypeHTTPStreamCancel {
			agentErr <- errUnexpectedType(cancelEnv.Type, messages.TypeHTTPStreamCancel)
			return
		}
		if cancelEnv.StreamID != reqEnv.StreamID {
			agentErr <- errString("cancel stream id did not match request")
			return
		}
		payload, err := messages.DecodePayload[messages.StreamCancel](cancelEnv)
		if err != nil {
			agentErr <- err
			return
		}
		if payload.Reason != "gateway stream timeout" {
			agentErr <- errString("cancel reason was not gateway stream timeout")
			return
		}
		agentErr <- nil
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/slow", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"

	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", resp.StatusCode)
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestPublicRequestRejectsWhenStreamLimitExceeded(t *testing.T) {
	cfg := testConfig()
	cfg.MaxConcurrentStreams = 1
	server := NewServer(cfg, slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	firstRequest := make(chan messages.Envelope, 1)
	agentErr := make(chan error, 1)
	releaseFirst := make(chan struct{})
	go func() {
		reqEnv, _, _, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		firstRequest <- reqEnv

		select {
		case <-releaseFirst:
		case <-ctx.Done():
			agentErr <- ctx.Err()
			return
		}

		resp := httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("first-ok"),
		}
		agentErr <- writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, resp)
	}()

	firstDone := make(chan *http.Response, 1)
	firstErr := make(chan error, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/first", nil)
		if err != nil {
			firstErr <- err
			return
		}
		req.Host = "demo.localhost"
		resp, err := publicServer.Client().Do(req)
		if err != nil {
			firstErr <- err
			return
		}
		firstDone <- resp
	}()

	select {
	case <-firstRequest:
	case err := <-firstErr:
		t.Fatalf("first request returned error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("agent did not receive first request")
	}

	secondReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/second", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	secondReq.Host = "demo.localhost"
	secondResp, err := publicServer.Client().Do(secondReq)
	if err != nil {
		t.Fatalf("second public request returned error: %v", err)
	}
	secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second status = %d, want 503", secondResp.StatusCode)
	}

	close(releaseFirst)
	select {
	case resp := <-firstDone:
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("first status = %d, want 200", resp.StatusCode)
		}
	case err := <-firstErr:
		t.Fatalf("first request returned error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("first request did not complete")
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestPublicRequestRejectsWhenRateLimitExceeded(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRequestsPerSecond = 1
	cfg.RateLimitBurst = 1
	server := NewServer(cfg, slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(agentServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	registerAgent(t, ctx, conn, "demo")

	secondForwarded := make(chan struct{})
	agentErr := make(chan error, 1)
	go func() {
		reqEnv, _, _, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		if err := writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("first-ok"),
		}); err != nil {
			agentErr <- err
			return
		}

		readCtx, cancelRead := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancelRead()
		secondReqEnv, _, _, err := readStreamedRequest(readCtx, conn)
		if err != nil {
			agentErr <- nil
			return
		}
		close(secondForwarded)
		agentErr <- writeStreamedResponse(ctx, conn, secondReqEnv.StreamID, secondReqEnv.TunnelID, httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("second-ok"),
		})
	}()

	firstReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/first", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	firstReq.Host = "demo.localhost"
	firstResp, err := publicServer.Client().Do(firstReq)
	if err != nil {
		t.Fatalf("first public request returned error: %v", err)
	}
	firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d, want 200", firstResp.StatusCode)
	}

	secondReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/second", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	secondReq.Host = "demo.localhost"
	secondResp, err := publicServer.Client().Do(secondReq)
	if err != nil {
		t.Fatalf("second public request returned error: %v", err)
	}
	secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", secondResp.StatusCode)
	}
	if secondResp.Header.Get("Retry-After") == "" {
		t.Fatal("second response missing Retry-After header")
	}

	select {
	case <-secondForwarded:
		t.Fatal("rate-limited request was forwarded to the agent")
	default:
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestAgentWebSocketRejectsInvalidToken(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	server := NewServer(testConfig(), logger)
	httpServer := httptest.NewServer(server.AgentHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, websocketURL(httpServer.URL, agentWebSocketPath), nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "wrong",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthError {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthError)
	}

	got := logs.String()
	if !strings.Contains(got, "event=gateway.agent_auth_failed") {
		t.Fatalf("logs = %q, want gateway.agent_auth_failed event", got)
	}
	if strings.Contains(got, "wrong") {
		t.Fatalf("logs leaked plaintext token: %q", got)
	}
}
func registerAgent(t *testing.T, ctx context.Context, conn *websocket.Conn, subdomain string) {
	t.Helper()
	_ = registerAgentAndRead(t, ctx, conn, subdomain)
}

func registerAgentAndRead(t *testing.T, ctx context.Context, conn *websocket.Conn, subdomain string) messages.TunnelRegistered {
	t.Helper()
	authenticateAgentForTest(t, ctx, conn)
	writeRegistrationForTest(t, ctx, conn, subdomain)

	var regResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &regResp); err != nil {
		t.Fatalf("read registration response returned error: %v", err)
	}
	if regResp.Type != messages.TypeTunnelRegistered {
		t.Fatalf("registration response type = %s, want %s", regResp.Type, messages.TypeTunnelRegistered)
	}

	payload, err := messages.DecodePayload[messages.TunnelRegistered](regResp)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	return payload
}

func authenticateAgentForTest(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "dev-token",
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatalf("write auth returned error: %v", err)
	}

	var authResp messages.Envelope
	if err := wsjson.Read(ctx, conn, &authResp); err != nil {
		t.Fatalf("read auth response returned error: %v", err)
	}
	if authResp.Type != messages.TypeAuthOK {
		t.Fatalf("auth response type = %s, want %s", authResp.Type, messages.TypeAuthOK)
	}
}

func writeRegistrationForTest(t *testing.T, ctx context.Context, conn *websocket.Conn, subdomain string) {
	t.Helper()
	reg, err := messages.New(messages.TypeTunnelRegister, messages.TunnelRegister{
		Protocol:           "http",
		LocalTarget:        "http://localhost:3000",
		RequestedSubdomain: subdomain,
	})
	if err != nil {
		t.Fatalf("New registration returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, reg); err != nil {
		t.Fatalf("write registration returned error: %v", err)
	}
}

func waitForPublicStatus(t *testing.T, ctx context.Context, client *http.Client, url, host string, want int) {
	t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("NewRequest returned error: %v", err)
		}
		req.Host = host

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == want {
				return
			}
		}

		select {
		case <-ctx.Done():
			if err != nil {
				t.Fatalf("public request did not reach status %d before timeout: %v", want, err)
			}
			t.Fatalf("public request did not reach status %d before timeout", want)
		case <-ticker.C:
		}
	}
}

func TestPublicHandlerHealthEndpoints(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.PublicHandler())
	defer httpServer.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := httpServer.Client().Get(httpServer.URL + path)
		if err != nil {
			t.Fatalf("GET %s returned error: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestPublicHandlerListsActiveTunnels(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	connectedAt := time.Date(2026, 6, 30, 11, 0, 0, 0, time.UTC)
	if err := server.registry.Register(&registry.Session{
		TunnelID:    "tun_active",
		Subdomain:   "demo",
		PublicURL:   "http://demo.localhost:8080",
		LocalTarget: "http://localhost:3000",
		Protocol:    "http",
		CreatedAt:   connectedAt,
	}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	httpServer := httptest.NewServer(server.PublicHandler())
	defer httpServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/tunnels", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Origin", "http://127.0.0.1:8082")
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("GET tunnels returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	var listed tunnelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(listed.Tunnels) != 1 {
		t.Fatalf("tunnels = %d, want 1", len(listed.Tunnels))
	}
	tunnel := listed.Tunnels[0]
	if tunnel.TunnelID != "tun_active" || tunnel.Subdomain != "demo" || tunnel.PublicURL != "http://demo.localhost:8080" || tunnel.Protocol != "http" {
		t.Fatalf("tunnel = %+v, want active demo tunnel", tunnel)
	}
	if !tunnel.ConnectedAt.Equal(connectedAt) {
		t.Fatalf("connected_at = %s, want %s", tunnel.ConnectedAt, connectedAt)
	}
}

func TestPublicHandlerMetricsEndpoint(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.PublicHandler())
	defer httpServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/missing", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	resp, err = httpServer.Client().Get(httpServer.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET metrics returned error: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200; body = %q", resp.StatusCode, string(body))
	}
	for _, want := range []string{
		"porthook_gateway_active_tunnels 0",
		"porthook_gateway_uptime_seconds ",
		"porthook_gateway_public_requests_total 1",
		"porthook_gateway_public_request_errors_total 0",
		"porthook_gateway_public_request_no_active_session_total 1",
		"porthook_gateway_tunnel_registration_failures_total 0",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("metrics = %q, want %q", string(body), want)
		}
	}
}

func TestPublicRequestLogsMissingTunnel(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	server := NewServer(testConfig(), logger)
	httpServer := httptest.NewServer(server.PublicHandler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/missing", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"

	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	got := logs.String()
	for _, want := range []string{
		`msg="public request"`,
		"event=gateway.public_request",
		"host=demo.localhost",
		"status=404",
		"outcome=no_active_session",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs = %q, want %q", got, want)
		}
	}
}

func TestContextWithTimeoutAllowsZeroTimeout(t *testing.T) {
	ctx, cancel := contextWithTimeout(context.Background(), 0)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context was canceled immediately")
	default:
	}
}

func TestAgentSessionDeliveryAppliesBackpressure(t *testing.T) {
	ch := make(chan wswire.Message, 1)
	session := &agentSession{
		done: make(chan struct{}),
		pending: map[string]chan wswire.Message{
			"str_test": ch,
		},
	}

	env, err := messages.NewStream(messages.TypeHTTPResponseBody, "str_test", "tun_test", nil)
	if err != nil {
		t.Fatalf("NewStream returned error: %v", err)
	}
	msg := wswire.Message{Envelope: env, BinaryBody: true, Body: []byte("chunk")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !session.deliver(ctx, msg) {
		t.Fatal("first deliver returned false")
	}

	delivered := make(chan struct{})
	go func() {
		session.deliver(ctx, msg)
		close(delivered)
	}()

	select {
	case <-delivered:
		t.Fatal("deliver returned while pending channel was full")
	case <-time.After(25 * time.Millisecond):
	}

	<-ch
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("deliver did not complete after pending channel had capacity")
	}
}

func TestRequestContentLength(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("body"))
	if got := requestContentLength(req, 99); got != 4 {
		t.Fatalf("requestContentLength = %d, want 4", got)
	}

	req.ContentLength = -1
	if got := requestContentLength(req, 99); got != 99 {
		t.Fatalf("requestContentLength fallback = %d, want 99", got)
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}

func errUnexpectedType(got, want messages.Type) error {
	return errString("unexpected message type " + string(got) + ", want " + string(want))
}

func readStreamedRequest(ctx context.Context, conn *websocket.Conn) (messages.Envelope, httpwire.RequestStart, []byte, error) {
	startMsg, err := wswire.Read(ctx, conn)
	if err != nil {
		return messages.Envelope{}, httpwire.RequestStart{}, nil, err
	}
	startEnv := startMsg.Envelope
	if startEnv.Type != messages.TypeHTTPRequestStart {
		return messages.Envelope{}, httpwire.RequestStart{}, nil, errUnexpectedType(startEnv.Type, messages.TypeHTTPRequestStart)
	}

	start, err := messages.DecodePayload[httpwire.RequestStart](startEnv)
	if err != nil {
		return messages.Envelope{}, httpwire.RequestStart{}, nil, err
	}

	var body bytes.Buffer
	for {
		msg, err := wswire.Read(ctx, conn)
		if err != nil {
			return messages.Envelope{}, httpwire.RequestStart{}, nil, err
		}
		env := msg.Envelope
		if env.StreamID != startEnv.StreamID {
			return messages.Envelope{}, httpwire.RequestStart{}, nil, errString("stream id changed while reading request")
		}

		switch env.Type {
		case messages.TypeHTTPRequestBody:
			if msg.BinaryBody {
				body.Write(msg.Body)
			} else {
				chunk, err := messages.DecodePayload[httpwire.BodyChunk](env)
				if err != nil {
					return messages.Envelope{}, httpwire.RequestStart{}, nil, err
				}
				body.Write(chunk.Data)
			}
		case messages.TypeHTTPRequestEnd:
			return startEnv, start, body.Bytes(), nil
		default:
			return messages.Envelope{}, httpwire.RequestStart{}, nil, errUnexpectedType(env.Type, messages.TypeHTTPRequestBody)
		}
	}
}

func writeStreamedResponse(ctx context.Context, conn *websocket.Conn, streamID, tunnelID string, resp httpwire.Response) error {
	start, err := messages.NewStream(messages.TypeHTTPResponseStart, streamID, tunnelID, httpwire.ResponseStart{
		Status: resp.Status,
		Header: resp.Header,
	})
	if err != nil {
		return err
	}
	if err := wswire.WriteEnvelope(ctx, conn, start); err != nil {
		return err
	}
	if len(resp.Body) > 0 {
		if err := wswire.WriteBinaryBody(ctx, conn, messages.TypeHTTPResponseBody, streamID, tunnelID, resp.Body); err != nil {
			return err
		}
	}
	end, err := messages.NewStream(messages.TypeHTTPResponseEnd, streamID, tunnelID, nil)
	if err != nil {
		return err
	}
	return wswire.WriteEnvelope(ctx, conn, end)
}

func testConfig() Config {
	return Config{
		PublicAddr:       ":8080",
		AgentAddr:        ":8081",
		RootDomain:       "localhost",
		PublicURL:        "http://localhost:8080",
		StaticToken:      "dev-token",
		MaxBodyBytes:     defaultMaxBodyBytes,
		StreamChunkBytes: defaultStreamChunkBytes,
		StreamTimeout:    defaultStreamTimeout,
		ShutdownTimeout:  defaultShutdownTimeout,
	}
}

func websocketURL(httpURL, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}
