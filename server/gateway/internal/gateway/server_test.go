// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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
	cfg := testConfig()
	cfg.TrustedProxies = "127.0.0.0/8"
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
	waitForSession(t, ctx, server, "demo")

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
		if payload.Header.Get("X-Forwarded-For") != "198.51.100.10" {
			agentErr <- errString("resolved client IP was not forwarded")
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
	req.Header.Set("X-Forwarded-For", "198.51.100.10")

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

func TestPublicHandlerForwardsOperationalPaths(t *testing.T) {
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
	waitForSession(t, ctx, server, "demo")

	paths := []string{"/healthz", "/metrics", "/api/v1/example"}
	agentErr := make(chan error, 1)
	go func() {
		for _, wantPath := range paths {
			reqEnv, payload, _, err := readStreamedRequest(ctx, conn)
			if err != nil {
				agentErr <- err
				return
			}
			if payload.Path != wantPath {
				agentErr <- fmt.Errorf("forwarded path = %q, want %q", payload.Path, wantPath)
				return
			}
			if err := writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
				Status: http.StatusOK,
				Body:   []byte(wantPath),
			}); err != nil {
				agentErr <- err
				return
			}
		}
		agentErr <- nil
	}()

	for _, path := range paths {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+path, nil)
		if err != nil {
			t.Fatalf("NewRequest(%s) returned error: %v", path, err)
		}
		req.Host = "demo.localhost"
		resp, err := publicServer.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s returned error: %v", path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s response: %v", path, readErr)
		}
		if resp.StatusCode != http.StatusOK || string(body) != path {
			t.Fatalf("GET %s = %d %q, want 200 %q", path, resp.StatusCode, body, path)
		}
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

func TestPublicRequestEnforcesBasicAccessPolicy(t *testing.T) {
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer validator-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/tokens/validate":
			writeJSON(w, http.StatusOK, map[string]any{
				"valid":    true,
				"token_id": "tok_owner",
			})
		case "/api/v1/reserved-subdomains/authorize":
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": true,
			})
		case "/api/v1/access-policies/evaluate":
			var req evaluateAccessPolicyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.Subdomain != "demo" {
				http.Error(w, "unexpected subdomain", http.StatusBadRequest)
				return
			}
			allowed := req.BasicUsername == "admin" && req.BasicPassword == "secret"
			reason := ""
			if !allowed {
				reason = "basic_auth_required"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": allowed,
				"mode":    "basic_auth",
				"reason":  reason,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	cfg.StreamTimeout = 200 * time.Millisecond
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

	deniedReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/private", nil)
	if err != nil {
		t.Fatalf("NewRequest denied returned error: %v", err)
	}
	deniedReq.Host = "demo.localhost"
	deniedResp, err := publicServer.Client().Do(deniedReq)
	if err != nil {
		t.Fatalf("denied public request returned error: %v", err)
	}
	deniedResp.Body.Close()
	if deniedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("denied status = %d, want 401", deniedResp.StatusCode)
	}
	if got := deniedResp.Header.Get("WWW-Authenticate"); got != `Basic realm="Porthook tunnel"` {
		t.Fatalf("WWW-Authenticate = %q, want Basic realm", got)
	}

	agentErr := make(chan error, 1)
	go func() {
		reqEnv, payload, _, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		if payload.Path != "/private" {
			agentErr <- errString("unexpected forwarded path")
			return
		}
		if payload.Header.Get("Authorization") != "" {
			agentErr <- errString("gateway access Authorization header was forwarded")
			return
		}
		agentErr <- writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("ok"),
		})
	}()

	allowedReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/private", nil)
	if err != nil {
		t.Fatalf("NewRequest allowed returned error: %v", err)
	}
	allowedReq.Host = "demo.localhost"
	allowedReq.SetBasicAuth("admin", "secret")
	allowedResp, err := publicServer.Client().Do(allowedReq)
	if err != nil {
		t.Fatalf("allowed public request returned error: %v", err)
	}
	defer allowedResp.Body.Close()
	body, err := io.ReadAll(allowedResp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if allowedResp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("allowed response = %d %q, want 200 ok", allowedResp.StatusCode, string(body))
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestPublicRequestBasicAuthLogsNeverExposePlaintextCredentials(t *testing.T) {
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer validator-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/tokens/validate":
			writeJSON(w, http.StatusOK, map[string]any{
				"valid":    true,
				"token_id": "tok_owner",
			})
		case "/api/v1/reserved-subdomains/authorize":
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": true,
			})
		case "/api/v1/access-policies/evaluate":
			var req evaluateAccessPolicyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			allowed := req.BasicUsername == "admin" && req.BasicPassword == "correct-horse-battery-staple"
			reason := ""
			if !allowed {
				reason = "basic_auth_required"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": allowed,
				"mode":    "basic_auth",
				"reason":  reason,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	var logs logBuffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	cfg := testConfig()
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	server := NewServer(cfg, logger)
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
		agentErr <- writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("ok"),
		})
	}()

	wrongReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/private", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	wrongReq.Host = "demo.localhost"
	wrongReq.SetBasicAuth("admin", "guessed-wrong-password")
	wrongResp, err := publicServer.Client().Do(wrongReq)
	if err != nil {
		t.Fatalf("denied public request returned error: %v", err)
	}
	wrongResp.Body.Close()
	if wrongResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("denied status = %d, want 401", wrongResp.StatusCode)
	}

	allowedReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/private", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	allowedReq.Host = "demo.localhost"
	allowedReq.SetBasicAuth("admin", "correct-horse-battery-staple")
	allowedResp, err := publicServer.Client().Do(allowedReq)
	if err != nil {
		t.Fatalf("allowed public request returned error: %v", err)
	}
	allowedResp.Body.Close()
	if allowedResp.StatusCode != http.StatusOK {
		t.Fatalf("allowed status = %d, want 200", allowedResp.StatusCode)
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}

	got := waitForLog(t, &logs, "event=gateway.public_request")
	if strings.Contains(got, "guessed-wrong-password") {
		t.Fatalf("logs leaked the plaintext incorrect password: %q", got)
	}
	if strings.Contains(got, "correct-horse-battery-staple") {
		t.Fatalf("logs leaked the plaintext correct password: %q", got)
	}
	if strings.Contains(got, "Basic ") {
		t.Fatalf("logs leaked a raw Basic authorization header: %q", got)
	}
}

func TestPublicRequestThrottlesRepeatedFailedBasicAuth(t *testing.T) {
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer validator-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/tokens/validate":
			writeJSON(w, http.StatusOK, map[string]any{
				"valid":    true,
				"token_id": "tok_owner",
			})
		case "/api/v1/reserved-subdomains/authorize":
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": true,
			})
		case "/api/v1/access-policies/evaluate":
			var req evaluateAccessPolicyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			allowed := req.BasicUsername == "admin" && req.BasicPassword == "secret"
			reason := ""
			if !allowed {
				reason = "basic_auth_required"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": allowed,
				"mode":    "basic_auth",
				"reason":  reason,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	cfg.TrustedProxies = "127.0.0.0/8"
	cfg.AuthAttemptLimit = 2
	cfg.AuthAttemptWindow = time.Minute
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

	failedAuthRequest := func(t *testing.T, clientAddr, username, password string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/private", nil)
		if err != nil {
			t.Fatalf("NewRequest returned error: %v", err)
		}
		req.Host = "demo.localhost"
		req.Header.Set("X-Forwarded-For", clientAddr)
		req.SetBasicAuth(username, password)
		resp, err := publicServer.Client().Do(req)
		if err != nil {
			t.Fatalf("public request returned error: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	for i := 0; i < cfg.AuthAttemptLimit; i++ {
		if status := failedAuthRequest(t, "198.51.100.10", "admin", "wrong"); status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i, status)
		}
	}

	if status := failedAuthRequest(t, "198.51.100.10", "admin", "wrong"); status != http.StatusTooManyRequests {
		t.Fatalf("status after exceeding the attempt limit = %d, want 429", status)
	}

	// The same throttled address is blocked even with correct credentials
	// until the window elapses: a lockout that lets an attacker verify a
	// guessed password would defeat the point of the throttle.
	if status := failedAuthRequest(t, "198.51.100.10", "admin", "secret"); status != http.StatusTooManyRequests {
		t.Fatalf("status with correct credentials while throttled = %d, want 429", status)
	}

	// A different client address for the same subdomain is unaffected.
	if status := failedAuthRequest(t, "198.51.100.20", "admin", "wrong"); status != http.StatusUnauthorized {
		t.Fatalf("status for an unrelated client address = %d, want 401", status)
	}
}

func TestPublicRequestRoutesCustomDomainThroughControlPlane(t *testing.T) {
	lookupCalls := 0
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer validator-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/tokens/validate":
			writeJSON(w, http.StatusOK, map[string]any{
				"valid":    true,
				"token_id": "tok_owner",
			})
		case "/api/v1/reserved-subdomains/authorize":
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": true,
			})
		case "/api/v1/custom-domains/lookup":
			lookupCalls++
			var req lookupCustomDomainRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.Hostname != "preview.example.test" {
				http.Error(w, "unexpected hostname", http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"found":                 true,
				"hostname":              "preview.example.test",
				"custom_domain_id":      "cd_preview",
				"reserved_subdomain_id": "rs_demo",
				"subdomain":             "demo",
				"status":                "active",
			})
		case "/api/v1/access-policies/evaluate":
			var req evaluateAccessPolicyRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.Subdomain != "demo" {
				http.Error(w, "unexpected subdomain", http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": true,
				"mode":    "public",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
	cfg.StreamTimeout = 200 * time.Millisecond
	cfg.CustomDomainCacheTTL = time.Minute
	cfg.RequestLogLimit = 10
	server := NewServer(cfg, slog.Default())
	agentServer := httptest.NewServer(server.AgentHandler())
	defer agentServer.Close()
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()
	managementServer := httptest.NewServer(server.ManagementHandler())
	defer managementServer.Close()

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
		reqEnv, payload, _, err := readStreamedRequest(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		if payload.Path != "/custom" {
			agentErr <- errString("unexpected forwarded path")
			return
		}
		agentErr <- writeStreamedResponse(ctx, conn, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("custom ok"),
		})
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/custom", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "preview.example.test"
	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("custom domain request returned error: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "custom ok" {
		t.Fatalf("response = %d %q, want 200 custom ok", resp.StatusCode, string(body))
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("lookup calls = %d, want 1", lookupCalls)
	}

	logResp, err := managementServer.Client().Get(managementServer.URL + "/api/v1/request-logs?limit=1")
	if err != nil {
		t.Fatalf("GET request logs returned error: %v", err)
	}
	defer logResp.Body.Close()
	var logs requestLogsResponse
	if err := json.NewDecoder(logResp.Body).Decode(&logs); err != nil {
		t.Fatalf("decode request logs returned error: %v", err)
	}
	if len(logs.RequestLogs) != 1 || logs.RequestLogs[0].CustomDomain != "preview.example.test" {
		t.Fatalf("request logs = %+v, want custom domain", logs.RequestLogs)
	}
}

func TestRouteForHostReturnsCustomDomainMissReason(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	server.domains = &routeTestCustomDomainResolver{
		result: customDomainResolution{
			Found:    false,
			Reason:   "not_verified",
			Hostname: "preview.example.test",
			Status:   "pending_verification",
		},
	}

	route, ok, err := server.routeForHost(context.Background(), "preview.example.test")
	if err != nil {
		t.Fatalf("routeForHost returned error: %v", err)
	}
	if ok {
		t.Fatal("routeForHost returned ok for pending custom domain")
	}
	if route.CustomDomain != "preview.example.test" || route.MissReason != "not_verified" {
		t.Fatalf("route = %+v, want custom domain miss reason", route)
	}
	if got := customDomainMissOutcome(route.MissReason); got != "custom_domain_not_verified" {
		t.Fatalf("outcome = %q, want custom_domain_not_verified", got)
	}
}

func TestRouteForHostRejectsInvalidCustomDomainSubdomain(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	server.domains = &routeTestCustomDomainResolver{
		result: customDomainResolution{
			Found:     true,
			Hostname:  "preview.example.test",
			Subdomain: "bad_name",
			Status:    "active",
		},
	}

	_, ok, err := server.routeForHost(context.Background(), "preview.example.test")
	if err == nil {
		t.Fatal("routeForHost returned nil error for invalid subdomain")
	}
	if ok {
		t.Fatal("routeForHost returned ok for invalid subdomain")
	}
	if !strings.Contains(err.Error(), "invalid subdomain") {
		t.Fatalf("error = %q, want invalid subdomain", err.Error())
	}
}

func TestRouteForHostDoesNotLookupRootDomainAsCustomDomain(t *testing.T) {
	cfg := testConfig()
	cfg.RootDomain = "example.test"
	server := NewServer(cfg, slog.Default())
	resolver := &routeTestCustomDomainResolver{
		result: customDomainResolution{
			Found:     true,
			Hostname:  "example.test",
			Subdomain: "demo",
			Status:    "active",
		},
	}
	server.domains = resolver

	_, ok, err := server.routeForHost(context.Background(), "example.test")
	if err != nil {
		t.Fatalf("routeForHost returned error: %v", err)
	}
	if ok {
		t.Fatal("routeForHost routed the root domain")
	}
	if resolver.calls != 0 {
		t.Fatalf("custom domain lookup calls = %d, want 0", resolver.calls)
	}
}

func TestRequestLogsEndpointReturnsRecentPublicRequests(t *testing.T) {
	cfg := testConfig()
	cfg.RequestLogLimit = 2
	server := NewServer(cfg, slog.Default())
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()
	managementServer := httptest.NewServer(server.ManagementHandler())
	defer managementServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/missing?secret=value", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "missing.localhost"
	req.Header.Set("X-Request-ID", "req_logs")
	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	logResp, err := managementServer.Client().Get(managementServer.URL + "/api/v1/request-logs?limit=1")
	if err != nil {
		t.Fatalf("GET request logs returned error: %v", err)
	}
	defer logResp.Body.Close()
	body, err := io.ReadAll(logResp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if logResp.StatusCode != http.StatusOK {
		t.Fatalf("logs status = %d, want 200; body = %q", logResp.StatusCode, body)
	}
	if strings.Contains(string(body), "secret=value") {
		t.Fatalf("request logs leaked raw query: %q", body)
	}

	var payload requestLogsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode request logs returned error: %v", err)
	}
	if len(payload.RequestLogs) != 1 {
		t.Fatalf("request_logs = %d, want 1", len(payload.RequestLogs))
	}
	entry := payload.RequestLogs[0]
	if entry.Host != "missing.localhost" || entry.Path != "/missing" || !entry.QueryPresent || entry.RequestID != "req_logs" {
		t.Fatalf("entry = %+v, want host/path/query_present", entry)
	}
	if entry.Status != http.StatusNotFound || entry.Outcome != "no_active_session" {
		t.Fatalf("entry = %+v, want 404 no_active_session", entry)
	}
}

func TestPublicRequestWritesDurableRequestLog(t *testing.T) {
	store := &captureRequestLogStore{}
	server := newServerWithRequestLogWriter(testConfig(), slog.Default(), store)
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/missing?secret=value", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "missing.localhost"
	req.Header.Set("X-Request-ID", "req_durable")
	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	entries := store.entriesSnapshot()
	if len(entries) != 1 {
		t.Fatalf("durable entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.RequestID != "req_durable" || entry.Host != "missing.localhost" || entry.Path != "/missing" {
		t.Fatalf("durable entry = %+v, want request metadata", entry)
	}
	if !entry.QueryPresent || entry.Status != http.StatusNotFound || entry.Outcome != "no_active_session" {
		t.Fatalf("durable entry = %+v, want safe query marker and 404 outcome", entry)
	}
}

func TestRequestLogsEndpointFiltersPublicRequests(t *testing.T) {
	cfg := testConfig()
	cfg.RequestLogLimit = 5
	server := NewServer(cfg, slog.Default())
	publicServer := httptest.NewServer(server.ManagementHandler())
	defer publicServer.Close()

	now := time.Now().UTC().Truncate(time.Second)
	server.requestLogs.add(requestLogEntry{
		Time:      now.Add(-time.Minute),
		Method:    http.MethodGet,
		Host:      "demo.localhost",
		Path:      "/health",
		RequestID: "req_old",
		Subdomain: "demo",
		TunnelID:  "tun_demo",
		Status:    http.StatusOK,
		Outcome:   "completed",
	})
	server.requestLogs.add(requestLogEntry{
		Time:         now.Add(-30 * time.Second),
		Method:       http.MethodPost,
		Host:         "preview.example.test",
		Path:         "/hooks/stripe",
		RequestID:    "req_match",
		Subdomain:    "api",
		CustomDomain: "preview.example.test",
		TunnelID:     "tun_match",
		Status:       http.StatusBadGateway,
		Outcome:      "tunnel_error",
	})
	server.requestLogs.add(requestLogEntry{
		Time:      now,
		Method:    http.MethodPost,
		Host:      "other.example.test",
		Path:      "/hooks/stripe",
		RequestID: "req_newer",
		Subdomain: "api",
		TunnelID:  "tun_other",
		Status:    http.StatusOK,
		Outcome:   "completed",
	})

	query := url.Values{}
	query.Set("limit", "1")
	query.Set("subdomain", "api")
	query.Set("method", "post")
	query.Set("host", "preview")
	query.Set("path", "hooks")
	query.Set("status", "502")
	query.Set("outcome", "tunnel")
	query.Set("request_id", "match")
	query.Set("tunnel_id", "tun_")
	query.Set("since", now.Add(-45*time.Second).Format(time.RFC3339))
	resp, err := publicServer.Client().Get(publicServer.URL + "/api/v1/request-logs?" + query.Encode())
	if err != nil {
		t.Fatalf("GET request logs returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload requestLogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request logs returned error: %v", err)
	}
	if len(payload.RequestLogs) != 1 {
		t.Fatalf("request_logs = %d, want 1", len(payload.RequestLogs))
	}
	if payload.RequestLogs[0].RequestID != "req_match" {
		t.Fatalf("request log = %+v, want req_match after filtering", payload.RequestLogs[0])
	}
}

func TestRequestLogsEndpointPaginatesPublicRequests(t *testing.T) {
	cfg := testConfig()
	cfg.RequestLogLimit = 5
	server := NewServer(cfg, slog.Default())
	publicServer := httptest.NewServer(server.ManagementHandler())
	defer publicServer.Close()

	now := time.Now().UTC().Truncate(time.Second)
	server.requestLogs.add(requestLogEntry{
		Time:      now.Add(-time.Minute),
		Method:    http.MethodGet,
		Host:      "api.localhost",
		Path:      "/old",
		RequestID: "req_old",
		Subdomain: "api",
		TunnelID:  "tun_api",
		Status:    http.StatusOK,
		Outcome:   "completed",
	})
	server.requestLogs.add(requestLogEntry{
		Time:      now,
		Method:    http.MethodGet,
		Host:      "api.localhost",
		Path:      "/new",
		RequestID: "req_new",
		Subdomain: "api",
		TunnelID:  "tun_api",
		Status:    http.StatusOK,
		Outcome:   "completed",
	})

	resp, err := publicServer.Client().Get(publicServer.URL + "/api/v1/request-logs?limit=1&subdomain=api")
	if err != nil {
		t.Fatalf("GET request logs returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var first requestLogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		t.Fatalf("decode first page returned error: %v", err)
	}
	if len(first.RequestLogs) != 1 || first.RequestLogs[0].RequestID != "req_new" {
		t.Fatalf("first page = %+v, want newest request", first.RequestLogs)
	}
	if first.NextCursor == "" {
		t.Fatal("next cursor is empty, want second page cursor")
	}
	if first.Filters.Limit != 1 || first.Filters.Subdomain != "api" {
		t.Fatalf("filters = %+v, want echoed limit and subdomain", first.Filters)
	}

	nextResp, err := publicServer.Client().Get(publicServer.URL + "/api/v1/request-logs?limit=1&subdomain=api&cursor=" + first.NextCursor)
	if err != nil {
		t.Fatalf("GET second page returned error: %v", err)
	}
	defer nextResp.Body.Close()
	if nextResp.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d, want 200", nextResp.StatusCode)
	}
	var second requestLogsResponse
	if err := json.NewDecoder(nextResp.Body).Decode(&second); err != nil {
		t.Fatalf("decode second page returned error: %v", err)
	}
	if len(second.RequestLogs) != 1 || second.RequestLogs[0].RequestID != "req_old" {
		t.Fatalf("second page = %+v, want older request", second.RequestLogs)
	}
	if second.Filters.Cursor != first.NextCursor {
		t.Fatalf("cursor filter = %q, want %q", second.Filters.Cursor, first.NextCursor)
	}
}

func TestRequestLogsEndpointReadsDurableStore(t *testing.T) {
	store := &captureRequestLogStore{
		entries: []requestLogEntry{
			{
				ID:        1,
				Time:      time.Now().UTC(),
				Method:    http.MethodGet,
				Host:      "durable.localhost",
				Path:      "/from-store",
				RequestID: "req_durable_store",
				Subdomain: "durable",
				TunnelID:  "tun_durable",
				Status:    http.StatusAccepted,
				Outcome:   "completed",
			},
		},
	}
	server := newServerWithRequestLogWriter(testConfig(), slog.Default(), store)
	publicServer := httptest.NewServer(server.ManagementHandler())
	defer publicServer.Close()

	resp, err := publicServer.Client().Get(publicServer.URL + "/api/v1/request-logs?limit=10")
	if err != nil {
		t.Fatalf("GET request logs returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload requestLogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request logs returned error: %v", err)
	}
	if len(payload.RequestLogs) != 1 || payload.RequestLogs[0].RequestID != "req_durable_store" {
		t.Fatalf("request logs = %+v, want durable store entry", payload.RequestLogs)
	}
	if payload.Filters.Limit != 10 {
		t.Fatalf("filters = %+v, want limit 10", payload.Filters)
	}
}

func TestRequestLogsEndpointRejectsInvalidFilters(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	publicServer := httptest.NewServer(server.ManagementHandler())
	defer publicServer.Close()

	cases := []struct {
		name  string
		query string
	}{
		{name: "status", query: "status=abc"},
		{name: "since", query: "since=not-a-time"},
		{name: "time range", query: "since=2026-07-01T12%3A00%3A00Z&until=2026-07-01T11%3A00%3A00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := publicServer.Client().Get(publicServer.URL + "/api/v1/request-logs?" + tc.query)
			if err != nil {
				t.Fatalf("GET request logs returned error: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestReadyzReportsRequestLogStoreFailure(t *testing.T) {
	store := &captureRequestLogStore{pingErr: errors.New("database unavailable")}
	server := newServerWithRequestLogWriter(testConfig(), slog.Default(), store)
	publicServer := httptest.NewServer(server.ManagementHandler())
	defer publicServer.Close()

	resp, err := publicServer.Client().Get(publicServer.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET readyz returned error: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %q", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "request log store") {
		t.Fatalf("body = %q, want request log store error", body)
	}
}

func TestRequestLogPrunerUsesDurableStore(t *testing.T) {
	now := time.Now().UTC()
	store := &captureRequestLogStore{
		entries: []requestLogEntry{
			{ID: 1, Time: now.Add(-2 * time.Hour), RequestID: "req_old"},
			{ID: 2, Time: now, RequestID: "req_new"},
		},
	}
	cfg := testConfig()
	cfg.RequestLogRetention = time.Hour
	server := newServerWithRequestLogWriter(cfg, slog.Default(), store)

	server.pruneRequestLogs(context.Background(), store)

	entries := store.entriesSnapshot()
	if len(entries) != 1 || entries[0].RequestID != "req_new" {
		t.Fatalf("entries = %+v, want only retained durable request log", entries)
	}
	if server.requestLogs.count() != 0 {
		t.Fatalf("memory request logs = %d, want durable pruning to leave memory buffer untouched", server.requestLogs.count())
	}
}

func TestPublicHandlerReturnsRequestIDOnErrors(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, publicServer.URL+"/missing", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "missing.localhost"
	req.Header.Set("X-Request-ID", "req_test")
	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.Header.Get("X-Request-ID") != "req_test" {
		t.Fatalf("X-Request-ID = %q, want req_test", resp.Header.Get("X-Request-ID"))
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(string(body), "request_id: req_test") {
		t.Fatalf("body = %q, want request id", string(body))
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
	waitForSession(t, ctx, server, "demo")

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

func TestPublicRequestLogsResolvedClientIP(t *testing.T) {
	tests := []struct {
		name           string
		trustedProxies string
		remoteAddr     string
		forwardedFor   string
		want           string
	}{
		{
			name:           "trusted proxy chain",
			trustedProxies: "10.0.0.0/8",
			remoteAddr:     "10.0.0.3:443",
			forwardedFor:   "198.51.100.10, 10.0.0.2",
			want:           "198.51.100.10",
		},
		{
			name:         "untrusted spoof",
			remoteAddr:   "203.0.113.9:443",
			forwardedFor: "198.51.100.10",
			want:         "203.0.113.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.TrustedProxies = tt.trustedProxies
			cfg.RequestLogLimit = 10
			server := NewServer(cfg, slog.Default())
			req := httptest.NewRequest(http.MethodGet, "http://missing.localhost/client-ip", nil)
			req.Host = "missing.localhost"
			req.RemoteAddr = tt.remoteAddr
			req.Header.Set("X-Forwarded-For", tt.forwardedFor)

			server.PublicHandler().ServeHTTP(httptest.NewRecorder(), req)

			logs := server.requestLogs.list(1)
			if len(logs) != 1 {
				t.Fatalf("request logs = %d, want 1", len(logs))
			}
			if got := logs[0].RemoteIP; got != tt.want {
				t.Fatalf("RemoteIP = %q, want %q", got, tt.want)
			}
		})
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
	waitForSession(t, ctx, server, "demo")

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
	waitForSession(t, ctx, server, "demo")

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
	waitForSession(t, ctx, server, "demo")

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
	var logs logBuffer
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

	got := waitForLog(t, &logs, "event=gateway.agent_auth_failed")
	if strings.Contains(got, "wrong") {
		t.Fatalf("logs leaked plaintext token: %q", got)
	}
}

type logBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func waitForLog(t *testing.T, logs *logBuffer, want string) string {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		got := logs.String()
		if strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("logs = %q, want %q", got, want)
		}
		time.Sleep(10 * time.Millisecond)
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

func waitForSession(t *testing.T, ctx context.Context, server *Server, subdomain string) {
	t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, ok := server.sessionBySubdomain(subdomain); ok {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("session %q did not register before timeout", subdomain)
		case <-ticker.C:
		}
	}
}

func TestManagementHandlerHealthEndpoints(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.ManagementHandler())
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

func TestManagementHandlerRequiresBearerForDataEndpoints(t *testing.T) {
	cfg := testConfig()
	cfg.ManagementToken = "management-secret"
	server := NewServer(cfg, slog.Default())
	httpServer := httptest.NewServer(server.ManagementHandler())
	defer httpServer.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := httpServer.Client().Get(httpServer.URL + path)
		if err != nil {
			t.Fatalf("GET %s without token returned error: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s without token = %d, want 200", path, resp.StatusCode)
		}
	}

	for _, path := range []string{"/api/v1/tunnels", "/api/v1/runtime", "/api/v1/request-logs", "/metrics"} {
		t.Run(path, func(t *testing.T) {
			resp, err := httpServer.Client().Get(httpServer.URL + path)
			if err != nil {
				t.Fatalf("GET without token returned error: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status without token = %d, want 401", resp.StatusCode)
			}
			if got := resp.Header.Get("WWW-Authenticate"); got != `Bearer realm="Porthook gateway management"` {
				t.Fatalf("WWW-Authenticate = %q", got)
			}

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+path, nil)
			if err != nil {
				t.Fatalf("NewRequest returned error: %v", err)
			}
			req.Header.Set("Authorization", "Bearer management-secret")
			resp, err = httpServer.Client().Do(req)
			if err != nil {
				t.Fatalf("GET with token returned error: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status with token = %d, want 200", resp.StatusCode)
			}
		})
	}
}

func TestManagementHandlerListsActiveTunnels(t *testing.T) {
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

	httpServer := httptest.NewServer(server.ManagementHandler())
	defer httpServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpServer.URL+"/api/v1/tunnels", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatalf("GET tunnels returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
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

func TestManagementHandlerShowsTunnelDetail(t *testing.T) {
	cfg := testConfig()
	cfg.RequestLogLimit = 10
	server := NewServer(cfg, slog.Default())
	connectedAt := time.Now().UTC().Add(-2 * time.Minute)
	tunnel := &registry.Session{
		TunnelID:        "tun_active",
		Subdomain:       "demo",
		PublicURL:       "http://demo.localhost:8080",
		LocalTarget:     "http://localhost:3000",
		Protocol:        "http",
		AgentVersion:    "test-agent",
		ProtocolVersion: messages.ProtocolVersion,
		CreatedAt:       connectedAt,
	}
	if err := server.registry.Register(tunnel); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	server.addSession(newAgentSession(nil, tunnel, time.Second, 4, 60, 120))
	requestAt := time.Now().UTC()
	server.requestLogs.add(requestLogEntry{
		Time:         requestAt,
		Method:       http.MethodGet,
		Host:         "preview.example.test",
		Path:         "/demo",
		RequestID:    "req_tunnel",
		Subdomain:    "demo",
		CustomDomain: "preview.example.test",
		TunnelID:     "tun_active",
		Status:       http.StatusOK,
		Outcome:      "proxied",
	})

	httpServer := httptest.NewServer(server.ManagementHandler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/tunnels/tun_active")
	if err != nil {
		t.Fatalf("GET tunnel detail returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var detail tunnelDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if detail.Tunnel.TunnelID != "tun_active" || detail.Tunnel.Subdomain != "demo" || detail.Tunnel.AgentVersion != "test-agent" {
		t.Fatalf("detail = %+v, want active tunnel metadata", detail.Tunnel)
	}
	if detail.Tunnel.ProtocolVersion != messages.ProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", detail.Tunnel.ProtocolVersion, messages.ProtocolVersion)
	}
	if detail.Tunnel.ActiveStreams != 0 || detail.Tunnel.StreamCapacity != 4 {
		t.Fatalf("stream stats = %d/%d, want 0/4", detail.Tunnel.ActiveStreams, detail.Tunnel.StreamCapacity)
	}
	if detail.Tunnel.ConnectedSeconds < 1 {
		t.Fatalf("connected seconds = %d, want positive uptime", detail.Tunnel.ConnectedSeconds)
	}
	if detail.Tunnel.RecentRequests.Count != 1 || detail.Tunnel.RecentRequests.LastStatus != http.StatusOK || detail.Tunnel.RecentRequests.LastRequestID != "req_tunnel" {
		t.Fatalf("request summary = %+v, want last request metadata", detail.Tunnel.RecentRequests)
	}
	if len(detail.Tunnel.RecentRequests.CustomDomains) != 1 || detail.Tunnel.RecentRequests.CustomDomains[0] != "preview.example.test" {
		t.Fatalf("custom domains = %+v, want preview.example.test", detail.Tunnel.RecentRequests.CustomDomains)
	}
}

func TestManagementHandlerTunnelDetailDoesNotExposeLocalTarget(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	tunnel := &registry.Session{
		TunnelID:    "tun_secret",
		Subdomain:   "demo",
		PublicURL:   "http://demo.localhost:8080",
		LocalTarget: "http://localhost:3000/private",
		Protocol:    "http",
		CreatedAt:   time.Now().UTC(),
	}
	if err := server.registry.Register(tunnel); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	server.addSession(newAgentSession(nil, tunnel, time.Second, 4, 60, 120))

	httpServer := httptest.NewServer(server.ManagementHandler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/tunnels/tun_secret")
	if err != nil {
		t.Fatalf("GET tunnel detail returned error: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if strings.Contains(string(body), "localhost:3000") || strings.Contains(string(body), "local_target") {
		t.Fatalf("body = %q, want no local target", string(body))
	}
}

func TestManagementHandlerTunnelDetailNotFound(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	httpServer := httptest.NewServer(server.ManagementHandler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/tunnels/missing")
	if err != nil {
		t.Fatalf("GET tunnel detail returned error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestManagementHandlerShowsRuntimeSummary(t *testing.T) {
	cfg := testConfig()
	cfg.ControlPlaneURL = "http://control-plane.internal"
	cfg.ControlPlaneToken = "control-secret"
	cfg.RequestLogLimit = 3
	cfg.MaxConcurrentStreams = 2
	cfg.RateLimitRequestsPerSecond = 11
	cfg.RateLimitBurst = 22
	server := NewServer(cfg, slog.Default())
	tunnel := &registry.Session{
		TunnelID:    "tun_runtime",
		Subdomain:   "runtime",
		PublicURL:   "http://runtime.localhost:8080",
		LocalTarget: "http://localhost:3000",
		Protocol:    "http",
		CreatedAt:   time.Now().UTC(),
	}
	if err := server.registry.Register(tunnel); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	server.addSession(newAgentSession(nil, tunnel, time.Second, cfg.MaxConcurrentStreams, cfg.RateLimitRequestsPerSecond, cfg.RateLimitBurst))
	server.requestLogs.add(requestLogEntry{
		Time:      time.Now().UTC(),
		Method:    http.MethodGet,
		Host:      "runtime.localhost",
		Path:      "/",
		Subdomain: "runtime",
		TunnelID:  "tun_runtime",
		Status:    http.StatusOK,
		Outcome:   "completed",
	})
	server.metrics.publicRequestsTotal.Add(3)
	server.metrics.publicRequestErrorsTotal.Add(1)
	server.metrics.tokenValidationsTotal.Add(2)

	httpServer := httptest.NewServer(server.ManagementHandler())
	defer httpServer.Close()

	resp, err := httpServer.Client().Get(httpServer.URL + "/api/v1/runtime")
	if err != nil {
		t.Fatalf("GET runtime returned error: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", resp.StatusCode, body)
	}
	for _, secret := range []string{"control-secret", "control-plane.internal", "localhost:3000"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("runtime body leaked %q: %q", secret, body)
		}
	}

	var payload runtimeSummaryResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode runtime returned error: %v", err)
	}
	runtime := payload.Runtime
	if runtime.ActiveTunnels != 1 || runtime.ActiveStreams != 0 || runtime.StreamCapacity != cfg.MaxConcurrentStreams {
		t.Fatalf("runtime stream stats = %+v, want one tunnel with stream capacity", runtime)
	}
	if !runtime.ControlPlaneConfigured || runtime.RequestLogEntries != 1 || runtime.RequestLogCapacity != cfg.RequestLogLimit {
		t.Fatalf("runtime = %+v, want configured control plane and request log usage", runtime)
	}
	if runtime.Limits.RateLimitRequestsPerSecond != 11 || runtime.Limits.RateLimitBurst != 22 {
		t.Fatalf("limits = %+v, want configured rate limits", runtime.Limits)
	}
	if runtime.Counters.PublicRequestsTotal != 3 || runtime.Counters.PublicRequestErrorsTotal != 1 || runtime.Counters.TokenValidationsTotal != 2 {
		t.Fatalf("counters = %+v, want metric counters", runtime.Counters)
	}
}

func TestManagementHandlerMetricsEndpoint(t *testing.T) {
	server := NewServer(testConfig(), slog.Default())
	publicServer := httptest.NewServer(server.PublicHandler())
	defer publicServer.Close()
	managementServer := httptest.NewServer(server.ManagementHandler())
	defer managementServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, publicServer.URL+"/missing", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = "demo.localhost"
	resp, err := publicServer.Client().Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	resp, err = managementServer.Client().Get(managementServer.URL + "/metrics")
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
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("metrics CORS = %q, want empty", got)
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

	req, err = http.NewRequestWithContext(context.Background(), http.MethodOptions, managementServer.URL+"/metrics", nil)
	if err != nil {
		t.Fatalf("NewRequest metrics OPTIONS returned error: %v", err)
	}
	resp, err = managementServer.Client().Do(req)
	if err != nil {
		t.Fatalf("OPTIONS metrics returned error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("OPTIONS metrics status = %d, want 405", resp.StatusCode)
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
	req.Header.Set("X-Request-ID", "req_public_log")

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
		"request_id=req_public_log",
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

type routeTestCustomDomainResolver struct {
	calls  int
	result customDomainResolution
	err    error
}

func (r *routeTestCustomDomainResolver) ResolveCustomDomain(context.Context, string) (customDomainResolution, error) {
	r.calls++
	if r.err != nil {
		return customDomainResolution{}, r.err
	}
	return r.result, nil
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

type captureRequestLogStore struct {
	mu      sync.Mutex
	pingErr error
	addErr  error
	listErr error
	nextID  int64
	entries []requestLogEntry
}

func (s *captureRequestLogStore) Ping(context.Context) error {
	return s.pingErr
}

func (s *captureRequestLogStore) Add(_ context.Context, entry requestLogEntry) error {
	if s.addErr != nil {
		return s.addErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ID == 0 {
		s.nextID++
		entry.ID = s.nextID
	} else if entry.ID > s.nextID {
		s.nextID = entry.ID
	}
	s.entries = append(s.entries, entry)
	return nil
}

func (s *captureRequestLogStore) List(_ context.Context, opts requestLogListOptions) (requestLogListPage, error) {
	if s.listErr != nil {
		return requestLogListPage{}, s.listErr
	}
	s.mu.Lock()
	entries := append([]requestLogEntry(nil), s.entries...)
	s.mu.Unlock()

	limit := normalizedRequestLogLimit(opts.Limit)
	if limit <= 0 {
		return requestLogListPage{}, nil
	}
	out := make([]requestLogEntry, 0, min(limit+1, len(entries)))
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if opts.Cursor.ID > 0 && entry.ID >= opts.Cursor.ID {
			continue
		}
		if !opts.Filter.matches(entry) {
			continue
		}
		out = append(out, entry)
		if len(out) > limit {
			break
		}
	}
	return requestLogPage(out, limit), nil
}

func (s *captureRequestLogStore) PruneBefore(_ context.Context, cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	retained := s.entries[:0]
	var pruned int64
	for _, entry := range s.entries {
		if !entry.Time.IsZero() && entry.Time.Before(cutoff) {
			pruned++
			continue
		}
		retained = append(retained, entry)
	}
	s.entries = retained
	return pruned, nil
}

func (s *captureRequestLogStore) entriesSnapshot() []requestLogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]requestLogEntry(nil), s.entries...)
}

func testConfig() Config {
	return Config{
		PublicAddr:       ":8080",
		AgentAddr:        ":8081",
		ManagementAddr:   ":8082",
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
