// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wsproxy"
	"github.com/voiteco/porthook/protocol/wswire"
)

func TestPublicWebSocketRelaysTextAndBinaryMessages(t *testing.T) {
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
		agentErr <- simulateAgentWSEcho(ctx, conn, wsproxy.Accept{})
	}()

	publicConn, _, err := websocket.Dial(ctx, websocketURL(publicServer.URL, "/socket"), &websocket.DialOptions{Host: "demo.localhost"})
	if err != nil {
		t.Fatalf("public websocket dial returned error: %v", err)
	}
	defer publicConn.Close(websocket.StatusNormalClosure, "")

	if err := publicConn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatalf("write text message returned error: %v", err)
	}
	msgType, data, err := publicConn.Read(ctx)
	if err != nil {
		t.Fatalf("read text echo returned error: %v", err)
	}
	if msgType != websocket.MessageText || string(data) != "hello" {
		t.Fatalf("echo = (%v, %q), want (Text, hello)", msgType, string(data))
	}

	binaryPayload := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	if err := publicConn.Write(ctx, websocket.MessageBinary, binaryPayload); err != nil {
		t.Fatalf("write binary message returned error: %v", err)
	}
	msgType, data, err = publicConn.Read(ctx)
	if err != nil {
		t.Fatalf("read binary echo returned error: %v", err)
	}
	if msgType != websocket.MessageBinary || !bytes.Equal(data, binaryPayload) {
		t.Fatalf("echo = (%v, %x), want (Binary, %x)", msgType, data, binaryPayload)
	}

	if err := publicConn.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("public Close returned error: %v", err)
	}

	select {
	case err := <-agentErr:
		if err != nil {
			t.Fatalf("agent simulation returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for agent simulation to observe the close")
	}
}

func TestPublicWebSocketNegotiatesSubprotocol(t *testing.T) {
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
		agentErr <- simulateAgentWSEcho(ctx, conn, wsproxy.Accept{Subprotocol: "chat.v1"})
	}()

	publicConn, _, err := websocket.Dial(ctx, websocketURL(publicServer.URL, "/socket"), &websocket.DialOptions{
		Host:         "demo.localhost",
		Subprotocols: []string{"chat.v1"},
	})
	if err != nil {
		t.Fatalf("public websocket dial returned error: %v", err)
	}
	defer publicConn.Close(websocket.StatusNormalClosure, "")

	if got := publicConn.Subprotocol(); got != "chat.v1" {
		t.Fatalf("negotiated subprotocol = %q, want chat.v1", got)
	}

	_ = publicConn.Close(websocket.StatusNormalClosure, "done")
	<-agentErr
}

func TestPublicWebSocketRejectsUpgradeWithoutCapability(t *testing.T) {
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

	// Simulate an older agent that authenticates without websocket_tunnel.
	auth, err := messages.New(messages.TypeAuthRequest, messages.AuthRequest{
		Token:           "dev-token",
		ProtocolVersion: "0.2",
		Capabilities: []string{
			messages.CapabilityStreamStartEnd,
			messages.CapabilityBinaryBodyFrame,
			messages.CapabilityStreamCancel,
		},
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
	waitForSession(t, ctx, server, "demo")

	status := webSocketUpgradeStatus(t, ctx, publicServer.Client(), publicServer.URL+"/socket", "demo.localhost")
	if status != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", status)
	}
}

func TestPublicWebSocketMapsLocalDialFailureToBadGateway(t *testing.T) {
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
		openMsg, err := wswire.Read(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		if openMsg.Envelope.Type != messages.TypeWSOpen {
			agentErr <- errUnexpectedType(openMsg.Envelope.Type, messages.TypeWSOpen)
			return
		}
		wsErr, err := messages.NewStream(messages.TypeWSError, openMsg.Envelope.StreamID, openMsg.Envelope.TunnelID, messages.ErrorPayload{
			Code:    "local_dial_failed",
			Message: "dial local websocket endpoint: connection refused",
		})
		if err != nil {
			agentErr <- err
			return
		}
		agentErr <- wswire.WriteEnvelope(ctx, conn, wsErr)
	}()

	status := webSocketUpgradeStatus(t, ctx, publicServer.Client(), publicServer.URL+"/socket", "demo.localhost")
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", status)
	}
	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}
}

func TestPublicWebSocketEnforcesAccessPolicy(t *testing.T) {
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer validator-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/tokens/validate":
			writeJSON(w, http.StatusOK, map[string]any{"valid": true, "token_id": "tok_owner"})
		case "/api/v1/reserved-subdomains/authorize":
			writeJSON(w, http.StatusOK, map[string]any{"allowed": true})
		case "/api/v1/access-policies/evaluate":
			writeJSON(w, http.StatusOK, map[string]any{
				"allowed": false,
				"mode":    "basic_auth",
				"reason":  "basic_auth_required",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	cfg := testConfig()
	cfg.ControlPlaneURL = controlPlane.URL
	cfg.ControlPlaneToken = "validator-secret"
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

	status := webSocketUpgradeStatus(t, ctx, publicServer.Client(), publicServer.URL+"/socket", "demo.localhost")
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (websocket upgrade must respect access policies)", status)
	}
}

func TestPublicWebSocketIdleTimeoutClosesStalledStream(t *testing.T) {
	cfg := testConfig()
	cfg.StreamRequestTimeout = time.Hour
	cfg.StreamIdleTimeout = 60 * time.Millisecond
	cfg.StreamMaxLifetime = time.Hour
	cfg.RequestLogLimit = 10
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
		openMsg, err := wswire.Read(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		accept, err := messages.NewStream(messages.TypeWSAccept, openMsg.Envelope.StreamID, openMsg.Envelope.TunnelID, wsproxy.Accept{})
		if err != nil {
			agentErr <- err
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, accept); err != nil {
			agentErr <- err
			return
		}
		// Go silent: no further messages, so the idle timeout must close
		// the stream instead of the connection hanging indefinitely.
		msg, err := wswire.Read(ctx, conn)
		if err != nil {
			agentErr <- err
			return
		}
		if msg.Envelope.Type != messages.TypeWSCancel && msg.Envelope.Type != messages.TypeWSClose {
			agentErr <- errUnexpectedType(msg.Envelope.Type, messages.TypeWSCancel)
			return
		}
		agentErr <- nil
	}()

	publicConn, _, err := websocket.Dial(ctx, websocketURL(publicServer.URL, "/socket"), &websocket.DialOptions{Host: "demo.localhost"})
	if err != nil {
		t.Fatalf("public websocket dial returned error: %v", err)
	}
	defer publicConn.CloseNow()

	// The idle timeout should close the connection out from under the
	// client; the exact error is transport-dependent, so just confirm the
	// read eventually fails rather than hanging for the test's lifetime.
	_, _, readErr := publicConn.Read(ctx)
	if readErr == nil {
		t.Fatal("public read returned nil error, want the idle timeout to close the connection")
	}

	if err := <-agentErr; err != nil {
		t.Fatalf("agent goroutine returned error: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		logs := server.requestLogs.list(1)
		if len(logs) == 1 {
			if logs[0].Outcome != "stream_idle_timeout" {
				t.Fatalf("outcome = %q, want stream_idle_timeout", logs[0].Outcome)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the request log entry")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// simulateAgentWSEcho plays the agent's role for a single tunneled
// WebSocket stream: it accepts the open request with accept, then echoes
// every text/binary message back until the gateway signals close or cancel.
func simulateAgentWSEcho(ctx context.Context, conn *websocket.Conn, accept wsproxy.Accept) error {
	openMsg, err := wswire.Read(ctx, conn)
	if err != nil {
		return err
	}
	if openMsg.Envelope.Type != messages.TypeWSOpen {
		return errUnexpectedType(openMsg.Envelope.Type, messages.TypeWSOpen)
	}
	streamID := openMsg.Envelope.StreamID
	tunnelID := openMsg.Envelope.TunnelID

	acceptEnv, err := messages.NewStream(messages.TypeWSAccept, streamID, tunnelID, accept)
	if err != nil {
		return err
	}
	if err := wswire.WriteEnvelope(ctx, conn, acceptEnv); err != nil {
		return err
	}

	for {
		msg, err := wswire.Read(ctx, conn)
		if err != nil {
			return err
		}
		switch msg.Envelope.Type {
		case messages.TypeWSMessageText, messages.TypeWSMessageBinary:
			if err := wswire.WriteBinaryBody(ctx, conn, msg.Envelope.Type, streamID, tunnelID, msg.Body); err != nil {
				return err
			}
		case messages.TypeWSClose, messages.TypeWSCancel:
			return nil
		default:
			return errUnexpectedType(msg.Envelope.Type, messages.TypeWSMessageText)
		}
	}
}

// webSocketUpgradeStatus sends a request with WebSocket upgrade headers but
// does not perform the client-side WebSocket handshake, so it observes the
// server's plain HTTP response when the upgrade is rejected before Accept.
func webSocketUpgradeStatus(t *testing.T, ctx context.Context, client *http.Client, url, host string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Host = host
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("public request returned error: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
