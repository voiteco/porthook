// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/messages"
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

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{Token: "dev-token"})
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

func TestAgentWebSocketRejectsInvalidToken(t *testing.T) {
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

	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{Token: "wrong"})
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

func testConfig() Config {
	return Config{
		PublicAddr:      ":8080",
		AgentAddr:       ":8081",
		RootDomain:      "localhost",
		PublicURL:       "http://localhost:8080",
		StaticToken:     "dev-token",
		MaxBodyBytes:    defaultMaxBodyBytes,
		StreamTimeout:   defaultStreamTimeout,
		ShutdownTimeout: defaultShutdownTimeout,
	}
}

func websocketURL(httpURL, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}
