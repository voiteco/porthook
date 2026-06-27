// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"bytes"
	"context"
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
		var reqEnv messages.Envelope
		if err := wsjson.Read(ctx, conn, &reqEnv); err != nil {
			agentErr <- err
			return
		}
		if reqEnv.Type != messages.TypeHTTPRequest {
			agentErr <- errUnexpectedType(reqEnv.Type, messages.TypeHTTPRequest)
			return
		}

		payload, err := messages.DecodePayload[httpwire.Request](reqEnv)
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
		if string(payload.Body) != "payload" {
			agentErr <- errString("request body was not forwarded")
			return
		}

		resp, err := messages.NewStream(messages.TypeHTTPResponse, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
			Status: http.StatusAccepted,
			Header: http.Header{
				"Content-Type": []string{"text/plain"},
				"Connection":   []string{"close"},
			},
			Body: []byte("via-agent"),
		})
		if err != nil {
			agentErr <- err
			return
		}
		if err := wsjson.Write(ctx, conn, resp); err != nil {
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
		var reqEnv messages.Envelope
		if err := wsjson.Read(ctx, conn, &reqEnv); err != nil {
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
		var reqEnv messages.Envelope
		if err := wsjson.Read(ctx, conn, &reqEnv); err != nil {
			agentErr <- err
			return
		}
		if reqEnv.Type != messages.TypeHTTPRequest {
			agentErr <- errUnexpectedType(reqEnv.Type, messages.TypeHTTPRequest)
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
		var reqEnv messages.Envelope
		if err := wsjson.Read(ctx, conn, &reqEnv); err != nil {
			agentErr <- err
			return
		}
		if reqEnv.Type != messages.TypeHTTPRequest {
			agentErr <- errUnexpectedType(reqEnv.Type, messages.TypeHTTPRequest)
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
		var reqEnv messages.Envelope
		if err := wsjson.Read(ctx, conn, &reqEnv); err != nil {
			agentErr <- err
			return
		}
		if reqEnv.Type != messages.TypeHTTPRequest {
			agentErr <- errUnexpectedType(reqEnv.Type, messages.TypeHTTPRequest)
			return
		}
		firstRequest <- reqEnv

		select {
		case <-releaseFirst:
		case <-ctx.Done():
			agentErr <- ctx.Err()
			return
		}

		resp, err := messages.NewStream(messages.TypeHTTPResponse, reqEnv.StreamID, reqEnv.TunnelID, httpwire.Response{
			Status: http.StatusOK,
			Body:   []byte("first-ok"),
		})
		if err != nil {
			agentErr <- err
			return
		}
		agentErr <- wsjson.Write(ctx, conn, resp)
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
