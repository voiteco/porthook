// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wswire"
)

func TestBuildWebSocketURL(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		want      string
	}{
		{
			name:      "http",
			serverURL: "http://localhost:8081",
			want:      "ws://localhost:8081/agent/connect",
		},
		{
			name:      "https",
			serverURL: "https://tunnel.example.com",
			want:      "wss://tunnel.example.com/agent/connect",
		},
		{
			name:      "base path",
			serverURL: "https://example.com/base",
			want:      "wss://example.com/base/agent/connect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildWebSocketURL(tt.serverURL)
			if err != nil {
				t.Fatalf("BuildWebSocketURL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildWebSocketURLRejectsInvalidURL(t *testing.T) {
	if _, err := BuildWebSocketURL("ftp://localhost"); err == nil {
		t.Fatal("BuildWebSocketURL returned nil error")
	}
}

func TestFormatAuthError(t *testing.T) {
	err := formatAuthError(messages.ErrorPayload{Code: "invalid_token", Message: "invalid token"})
	if err == nil {
		t.Fatal("formatAuthError returned nil")
	}
	want := "authentication failed: invalid token; check --token or PORTHOOK_TOKEN"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestFormatTunnelRegistrationErrorForDuplicateSubdomain(t *testing.T) {
	err := formatTunnelRegistrationError(messages.ErrorPayload{
		Code:    "subdomain_unavailable",
		Message: `subdomain "demo" is already in use; choose another name or omit --subdomain for a random subdomain`,
	})
	if err == nil {
		t.Fatal("formatTunnelRegistrationError returned nil")
	}
	if !strings.Contains(err.Error(), `subdomain "demo" is already in use`) {
		t.Fatalf("error = %q, want duplicate subdomain guidance", err.Error())
	}
	if !strings.Contains(err.Error(), "omit --subdomain") {
		t.Fatalf("error = %q, want random subdomain guidance", err.Error())
	}
}

func TestWriteRequestOutputForFailure(t *testing.T) {
	var output bytes.Buffer
	runner := NewRunner(Config{}, nil, &output)

	runner.writeRequestOutput(httpwire.Request{
		Method: http.MethodGet,
		Path:   "/broken",
		Query:  "x=1",
	}, 0, 12*time.Millisecond, fmt.Errorf("local service failed"))

	got := output.String()
	if !strings.Contains(got, "GET /broken?x=1 -> error 12ms: local service failed") {
		t.Fatalf("output = %q, want failure request log", got)
	}
}

func TestRunnerHandlesHTTPRequest(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/webhook" {
			t.Errorf("path = %s, want /webhook", r.URL.Path)
		}
		if r.URL.RawQuery != "source=test" {
			t.Errorf("query = %s, want source=test", r.URL.RawQuery)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll returned error: %v", err)
		}
		if string(body) != "payload" {
			t.Errorf("body = %q, want payload", string(body))
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("local-ok"))
	}))
	defer local.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		var auth messages.Envelope
		if err := wsjson.Read(ctx, conn, &auth); err != nil {
			t.Errorf("read auth returned error: %v", err)
			return
		}
		if auth.Type != messages.TypeAuthRequest {
			t.Errorf("auth type = %s, want %s", auth.Type, messages.TypeAuthRequest)
			return
		}
		authPayload, err := messages.DecodePayload[messages.AuthRequest](auth)
		if err != nil {
			t.Errorf("decode auth request returned error: %v", err)
			return
		}
		if authPayload.ProtocolVersion != messages.ProtocolVersion {
			t.Errorf("auth protocol version = %q, want %q", authPayload.ProtocolVersion, messages.ProtocolVersion)
			return
		}
		if len(authPayload.Capabilities) != len(messages.DefaultProtocolCapabilities()) {
			t.Errorf("auth capabilities = %d, want %d", len(authPayload.Capabilities), len(messages.DefaultProtocolCapabilities()))
			return
		}
		authOK, _ := messages.New(messages.TypeAuthOK, messages.AuthOK{
			ProtocolVersion: messages.ProtocolVersion,
			Capabilities:    messages.DefaultProtocolCapabilities(),
		})
		if err := wsjson.Write(ctx, conn, authOK); err != nil {
			t.Errorf("write auth ok returned error: %v", err)
			return
		}

		var registration messages.Envelope
		if err := wsjson.Read(ctx, conn, &registration); err != nil {
			t.Errorf("read registration returned error: %v", err)
			return
		}
		if registration.Type != messages.TypeTunnelRegister {
			t.Errorf("registration type = %s, want %s", registration.Type, messages.TypeTunnelRegister)
			return
		}
		registered, _ := messages.New(messages.TypeTunnelRegistered, messages.TunnelRegistered{
			TunnelID:  "tun_test",
			PublicURL: "http://demo.localhost:8080",
			Subdomain: "demo",
		})
		if err := wsjson.Write(ctx, conn, registered); err != nil {
			t.Errorf("write registered returned error: %v", err)
			return
		}

		tunnelReq, err := messages.NewStream(messages.TypeHTTPRequest, "str_test", "tun_test", httpwire.Request{
			Method: http.MethodPost,
			Path:   "/webhook",
			Query:  "source=test",
			Header: http.Header{"X-Test": []string{"yes"}},
			Body:   []byte("payload"),
		})
		if err != nil {
			t.Errorf("NewStream returned error: %v", err)
			return
		}
		if err := wsjson.Write(ctx, conn, tunnelReq); err != nil {
			t.Errorf("write tunnel request returned error: %v", err)
			return
		}

		var response messages.Envelope
		if err := wsjson.Read(ctx, conn, &response); err != nil {
			t.Errorf("read tunnel response returned error: %v", err)
			return
		}
		if response.Type != messages.TypeHTTPResponse {
			t.Errorf("response type = %s, want %s", response.Type, messages.TypeHTTPResponse)
			return
		}
		if response.StreamID != "str_test" {
			t.Errorf("stream id = %s, want str_test", response.StreamID)
			return
		}
		payload, err := messages.DecodePayload[httpwire.Response](response)
		if err != nil {
			t.Errorf("DecodePayload returned error: %v", err)
			return
		}
		if payload.Status != http.StatusCreated {
			t.Errorf("status = %d, want 201", payload.Status)
		}
		if string(payload.Body) != "local-ok" {
			t.Errorf("body = %q, want local-ok", string(payload.Body))
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:          gateway.URL,
		Token:              "dev-token",
		RequestedSubdomain: "demo",
		LocalTarget:        local.URL,
		AgentVersion:       "test",
		RequestTimeout:     5 * time.Second,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("Tunnel established")) {
		t.Fatalf("output does not contain tunnel establishment: %q", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte("POST /webhook?source=test -> 201")) {
		t.Fatalf("output does not contain request log: %q", output.String())
	}
}

func TestRunnerHandlesStreamedHTTPRequest(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/webhook" {
			t.Errorf("path = %s, want /webhook", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll returned error: %v", err)
		}
		if string(body) != "payload" {
			t.Errorf("body = %q, want payload", string(body))
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("local-ok"))
	}))
	defer local.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		readAuthAndRegistration(t, ctx, conn)

		start, err := messages.NewStream(messages.TypeHTTPRequestStart, "str_stream", "tun_test", httpwire.RequestStart{
			Method:        http.MethodPost,
			Path:          "/webhook",
			Header:        http.Header{"X-Test": []string{"yes"}},
			ContentLength: 7,
		})
		if err != nil {
			t.Errorf("NewStream start returned error: %v", err)
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, start); err != nil {
			t.Errorf("write request start returned error: %v", err)
			return
		}
		for _, data := range []string{"pay", "load"} {
			if err := wswire.WriteBinaryBody(ctx, conn, messages.TypeHTTPRequestBody, "str_stream", "tun_test", []byte(data)); err != nil {
				t.Errorf("write request body returned error: %v", err)
				return
			}
		}
		end, err := messages.NewStream(messages.TypeHTTPRequestEnd, "str_stream", "tun_test", nil)
		if err != nil {
			t.Errorf("NewStream end returned error: %v", err)
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, end); err != nil {
			t.Errorf("write request end returned error: %v", err)
			return
		}

		var responseBody bytes.Buffer
		for {
			msg, err := wswire.Read(ctx, conn)
			if err != nil {
				t.Errorf("read response returned error: %v", err)
				return
			}
			response := msg.Envelope
			if response.StreamID != "str_stream" {
				t.Errorf("stream id = %s, want str_stream", response.StreamID)
				return
			}
			switch response.Type {
			case messages.TypeHTTPResponseStart:
				payload, err := messages.DecodePayload[httpwire.ResponseStart](response)
				if err != nil {
					t.Errorf("DecodePayload response start returned error: %v", err)
					return
				}
				if payload.Status != http.StatusCreated {
					t.Errorf("status = %d, want 201", payload.Status)
					return
				}
			case messages.TypeHTTPResponseBody:
				if msg.BinaryBody {
					responseBody.Write(msg.Body)
				} else {
					payload, err := messages.DecodePayload[httpwire.BodyChunk](response)
					if err != nil {
						t.Errorf("DecodePayload response body returned error: %v", err)
						return
					}
					responseBody.Write(payload.Data)
				}
			case messages.TypeHTTPResponseEnd:
				if responseBody.String() != "local-ok" {
					t.Errorf("response body = %q, want local-ok", responseBody.String())
				}
				return
			default:
				t.Errorf("response type = %s, want streaming response", response.Type)
				return
			}
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:          gateway.URL,
		Token:              "dev-token",
		RequestedSubdomain: "demo",
		LocalTarget:        local.URL,
		AgentVersion:       "test",
		RequestTimeout:     5 * time.Second,
		StreamChunkBytes:   4,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("POST /webhook -> 201")) {
		t.Fatalf("output does not contain request log: %q", output.String())
	}
}

func TestRunnerStreamsLocalResponseBeforeCompletion(t *testing.T) {
	releaseSecond := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseSecond) })

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("first-"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		select {
		case <-releaseSecond:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("second"))
	}))
	defer local.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		readAuthAndRegistration(t, ctx, conn)

		start, err := messages.NewStream(messages.TypeHTTPRequestStart, "str_slow", "tun_test", httpwire.RequestStart{
			Method: http.MethodGet,
			Path:   "/slow",
		})
		if err != nil {
			t.Errorf("NewStream start returned error: %v", err)
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, start); err != nil {
			t.Errorf("write request start returned error: %v", err)
			return
		}
		end, err := messages.NewStream(messages.TypeHTTPRequestEnd, "str_slow", "tun_test", nil)
		if err != nil {
			t.Errorf("NewStream end returned error: %v", err)
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, end); err != nil {
			t.Errorf("write request end returned error: %v", err)
			return
		}

		gotStart := false
		gotFirstChunk := false
		var responseBody bytes.Buffer
		for {
			msg, err := wswire.Read(ctx, conn)
			if err != nil {
				t.Errorf("read response returned error: %v", err)
				return
			}
			response := msg.Envelope
			if response.StreamID != "str_slow" {
				t.Errorf("stream id = %s, want str_slow", response.StreamID)
				return
			}

			switch response.Type {
			case messages.TypeHTTPResponseStart:
				payload, err := messages.DecodePayload[httpwire.ResponseStart](response)
				if err != nil {
					t.Errorf("DecodePayload response start returned error: %v", err)
					return
				}
				if payload.Status != http.StatusOK {
					t.Errorf("status = %d, want 200", payload.Status)
					return
				}
				gotStart = true
			case messages.TypeHTTPResponseBody:
				if !gotStart {
					t.Errorf("response body arrived before response start")
					return
				}
				if msg.BinaryBody {
					responseBody.Write(msg.Body)
				} else {
					payload, err := messages.DecodePayload[httpwire.BodyChunk](response)
					if err != nil {
						t.Errorf("DecodePayload response body returned error: %v", err)
						return
					}
					responseBody.Write(payload.Data)
				}
				if !gotFirstChunk && strings.Contains(responseBody.String(), "first-") {
					gotFirstChunk = true
					releaseOnce.Do(func() { close(releaseSecond) })
				}
			case messages.TypeHTTPResponseEnd:
				if !gotFirstChunk {
					t.Errorf("response ended before first chunk was observed")
					return
				}
				if responseBody.String() != "first-second" {
					t.Errorf("response body = %q, want first-second", responseBody.String())
				}
				return
			default:
				t.Errorf("response type = %s, want streaming response", response.Type)
				return
			}
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:          gateway.URL,
		Token:              "dev-token",
		RequestedSubdomain: "demo",
		LocalTarget:        local.URL,
		AgentVersion:       "test",
		RequestTimeout:     5 * time.Second,
		StreamChunkBytes:   64,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	waitForRunnerOutput(t, runner, &output, "GET /slow -> 200")
}

func TestRunnerHandlesConcurrentHTTPRequests(t *testing.T) {
	const requestCount = 6

	var startedMu sync.Mutex
	started := 0
	allStarted := make(chan struct{})

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedMu.Lock()
		started++
		if started == requestCount {
			close(allStarted)
		}
		startedMu.Unlock()

		select {
		case <-allStarted:
		case <-r.Context().Done():
			t.Errorf("request %s ended before all concurrent requests arrived", r.URL.Path)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "ok:%s", r.URL.Path)
	}))
	defer local.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		readAuthAndRegistration(t, ctx, conn)

		for i := 0; i < requestCount; i++ {
			streamID := fmt.Sprintf("str_%d", i)
			tunnelReq, err := messages.NewStream(messages.TypeHTTPRequest, streamID, "tun_test", httpwire.Request{
				Method: http.MethodGet,
				Path:   fmt.Sprintf("/request-%d", i),
			})
			if err != nil {
				t.Errorf("NewStream returned error: %v", err)
				return
			}
			if err := wsjson.Write(ctx, conn, tunnelReq); err != nil {
				t.Errorf("write tunnel request returned error: %v", err)
				return
			}
		}

		seen := make(map[string]bool)
		for len(seen) < requestCount {
			var response messages.Envelope
			if err := wsjson.Read(ctx, conn, &response); err != nil {
				t.Errorf("read tunnel response returned error: %v", err)
				return
			}
			if response.Type != messages.TypeHTTPResponse {
				t.Errorf("response type = %s, want %s", response.Type, messages.TypeHTTPResponse)
				return
			}
			payload, err := messages.DecodePayload[httpwire.Response](response)
			if err != nil {
				t.Errorf("DecodePayload returned error: %v", err)
				return
			}
			if payload.Status != http.StatusOK {
				t.Errorf("status = %d, want 200", payload.Status)
				return
			}
			seen[response.StreamID] = true
		}
	}))
	defer gateway.Close()

	runner := NewRunner(Config{
		ServerURL:          gateway.URL,
		Token:              "dev-token",
		RequestedSubdomain: "demo",
		LocalTarget:        local.URL,
		AgentVersion:       "test",
		RequestTimeout:     5 * time.Second,
	}, nil, io.Discard)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRunnerReconnectsAfterGatewayGoingAway(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/after-reconnect" {
			t.Errorf("path = %s, want /after-reconnect", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("restored-ok"))
	}))
	defer local.Close()

	var connections atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		readAuthAndRegistration(t, ctx, conn)

		if connections.Add(1) == 1 {
			_ = conn.Close(websocket.StatusGoingAway, "test restart")
			return
		}

		tunnelReq, err := messages.NewStream(messages.TypeHTTPRequest, "str_reconnect", "tun_test", httpwire.Request{
			Method: http.MethodGet,
			Path:   "/after-reconnect",
		})
		if err != nil {
			t.Errorf("NewStream returned error: %v", err)
			return
		}
		if err := wsjson.Write(ctx, conn, tunnelReq); err != nil {
			t.Errorf("write tunnel request returned error: %v", err)
			return
		}

		var response messages.Envelope
		if err := wsjson.Read(ctx, conn, &response); err != nil {
			t.Errorf("read tunnel response returned error: %v", err)
			return
		}
		if response.Type != messages.TypeHTTPResponse {
			t.Errorf("response type = %s, want %s", response.Type, messages.TypeHTTPResponse)
			return
		}
		payload, err := messages.DecodePayload[httpwire.Response](response)
		if err != nil {
			t.Errorf("DecodePayload returned error: %v", err)
			return
		}
		if payload.Status != http.StatusOK {
			t.Errorf("status = %d, want 200", payload.Status)
		}
		if string(payload.Body) != "restored-ok" {
			t.Errorf("body = %q, want restored-ok", string(payload.Body))
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:             gateway.URL,
		Token:                 "dev-token",
		RequestedSubdomain:    "demo",
		LocalTarget:           local.URL,
		AgentVersion:          "test",
		RequestTimeout:        5 * time.Second,
		WebSocketPingInterval: time.Hour,
		ReconnectInitialDelay: time.Millisecond,
		ReconnectMaxDelay:     5 * time.Millisecond,
		ReconnectJitter:       0,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("connections = %d, want 2", got)
	}

	got := output.String()
	for _, want := range []string{
		"Tunnel established",
		"Disconnected, reconnecting",
		"Tunnel restored",
		"GET /after-reconnect -> 200",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestRunnerDoesNotReconnectAfterAuthError(t *testing.T) {
	var connections atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}
		connections.Add(1)

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusPolicyViolation, "auth failed")

		var auth messages.Envelope
		if err := wsjson.Read(r.Context(), conn, &auth); err != nil {
			t.Errorf("read auth returned error: %v", err)
			return
		}

		authErr, _ := messages.New(messages.TypeAuthError, messages.ErrorPayload{
			Code:    "invalid_token",
			Message: "invalid token",
		})
		if err := wsjson.Write(r.Context(), conn, authErr); err != nil {
			t.Errorf("write auth error returned error: %v", err)
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:             gateway.URL,
		Token:                 "wrong",
		LocalTarget:           "http://localhost:3000",
		ReconnectInitialDelay: time.Millisecond,
		ReconnectMaxDelay:     time.Millisecond,
		ReconnectJitter:       0,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := runner.Run(ctx)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	if !strings.Contains(err.Error(), "authentication failed: invalid token") {
		t.Fatalf("error = %q, want auth error", err.Error())
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
	if strings.Contains(output.String(), "retrying") || strings.Contains(output.String(), "reconnecting") {
		t.Fatalf("output = %q, want no reconnect output", output.String())
	}
}

func TestRunnerDoesNotReconnectAfterUnsupportedProtocolAuthOK(t *testing.T) {
	var connections atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}
		connections.Add(1)

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusPolicyViolation, "auth failed")

		var auth messages.Envelope
		if err := wsjson.Read(r.Context(), conn, &auth); err != nil {
			t.Errorf("read auth returned error: %v", err)
			return
		}

		authPayload, err := messages.DecodePayload[messages.AuthRequest](auth)
		if err != nil {
			t.Errorf("decode auth request returned error: %v", err)
			return
		}
		if authPayload.Token != "dev-token" {
			t.Errorf("auth token = %q, want dev-token", authPayload.Token)
		}

		authOK, _ := messages.New(messages.TypeAuthOK, messages.AuthOK{
			ProtocolVersion: "0.1",
			Capabilities:    []string{"old"},
		})
		if err := wsjson.Write(r.Context(), conn, authOK); err != nil {
			t.Errorf("write auth ok returned error: %v", err)
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:             gateway.URL,
		Token:                 "dev-token",
		LocalTarget:           "http://localhost:3000",
		ReconnectInitialDelay: time.Millisecond,
		ReconnectMaxDelay:     time.Millisecond,
		ReconnectJitter:       0,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := runner.Run(ctx)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	if !strings.Contains(err.Error(), `gateway protocol version "0.1" is not supported`) {
		t.Fatalf("error = %q, want protocol mismatch error", err.Error())
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
	if strings.Contains(output.String(), "retrying") || strings.Contains(output.String(), "reconnecting") {
		t.Fatalf("output = %q, want no reconnect output", output.String())
	}
}

func TestRunnerAcceptsOlderCompatibleGatewayProtocol(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		var auth messages.Envelope
		if err := wsjson.Read(r.Context(), conn, &auth); err != nil {
			t.Errorf("read auth returned error: %v", err)
			return
		}
		if _, err := messages.DecodePayload[messages.AuthRequest](auth); err != nil {
			t.Errorf("decode auth request returned error: %v", err)
			return
		}

		// Simulate a gateway built before WebSocket tunneling existed: it
		// declares protocol 0.2 and only the baseline capabilities.
		authOK, _ := messages.New(messages.TypeAuthOK, messages.AuthOK{
			ProtocolVersion: "0.2",
			Capabilities: []string{
				messages.CapabilityStreamStartEnd,
				messages.CapabilityBinaryBodyFrame,
				messages.CapabilityStreamCancel,
			},
		})
		if err := wsjson.Write(r.Context(), conn, authOK); err != nil {
			t.Errorf("write auth ok returned error: %v", err)
			return
		}

		registration, err := messages.New(messages.TypeTunnelError, messages.ErrorPayload{
			Code:    "registration_failed",
			Message: "stop after negotiation for this test",
		})
		if err != nil {
			t.Errorf("New tunnel error returned error: %v", err)
			return
		}
		_ = wsjson.Write(r.Context(), conn, registration)
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:             gateway.URL,
		Token:                 "dev-token",
		LocalTarget:           "http://localhost:3000",
		ReconnectInitialDelay: time.Millisecond,
		ReconnectMaxDelay:     time.Millisecond,
		ReconnectJitter:       0,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := runner.Run(ctx)
	if err == nil {
		t.Fatal("Run returned nil error")
	}
	// Authentication itself must succeed against the older gateway; the
	// registration_failed error proves negotiation got past auth.
	if !strings.Contains(err.Error(), "tunnel registration failed") {
		t.Fatalf("error = %q, want tunnel registration failure, not an auth/protocol error", err.Error())
	}
}

func TestRunnerCancelsLocalRequestOnStreamCancel(t *testing.T) {
	localStarted := make(chan struct{})
	localCanceled := make(chan struct{})
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(localStarted)
		<-r.Context().Done()
		close(localCanceled)
	}))
	defer local.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		readAuthAndRegistration(t, ctx, conn)

		tunnelReq, err := messages.NewStream(messages.TypeHTTPRequest, "str_cancel", "tun_test", httpwire.Request{
			Method: http.MethodGet,
			Path:   "/cancel-me",
		})
		if err != nil {
			t.Errorf("NewStream returned error: %v", err)
			return
		}
		if err := wsjson.Write(ctx, conn, tunnelReq); err != nil {
			t.Errorf("write tunnel request returned error: %v", err)
			return
		}

		select {
		case <-localStarted:
		case <-time.After(time.Second):
			t.Errorf("local service did not receive request")
			return
		}

		cancelEnv, err := messages.NewStream(messages.TypeHTTPStreamCancel, "str_cancel", "tun_test", messages.StreamCancel{
			Reason: "test cancel",
		})
		if err != nil {
			t.Errorf("NewStream cancel returned error: %v", err)
			return
		}
		if err := wsjson.Write(ctx, conn, cancelEnv); err != nil {
			t.Errorf("write cancel returned error: %v", err)
			return
		}

		select {
		case <-localCanceled:
		case <-time.After(time.Second):
			t.Errorf("local request was not canceled")
			return
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:             gateway.URL,
		Token:                 "dev-token",
		RequestedSubdomain:    "demo",
		LocalTarget:           local.URL,
		AgentVersion:          "test",
		RequestTimeout:        5 * time.Second,
		WebSocketPingInterval: time.Hour,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	waitForRunnerOutput(t, runner, &output, "GET /cancel-me -> error")
}

func waitForRunnerOutput(t *testing.T, runner *Runner, output *bytes.Buffer, want string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		runner.outputMu.Lock()
		got := output.String()
		runner.outputMu.Unlock()
		if strings.Contains(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("output = %q, want %q", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readAuthAndRegistration(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()

	var auth messages.Envelope
	if err := wsjson.Read(ctx, conn, &auth); err != nil {
		t.Fatalf("read auth returned error: %v", err)
	}
	if auth.Type != messages.TypeAuthRequest {
		t.Fatalf("auth type = %s, want %s", auth.Type, messages.TypeAuthRequest)
	}
	authPayload, err := messages.DecodePayload[messages.AuthRequest](auth)
	if err != nil {
		t.Fatalf("decode auth request returned error: %v", err)
	}
	if authPayload.ProtocolVersion != messages.ProtocolVersion {
		t.Fatalf("auth protocol version = %q, want %q", authPayload.ProtocolVersion, messages.ProtocolVersion)
	}
	if len(authPayload.Capabilities) != len(messages.DefaultProtocolCapabilities()) {
		t.Fatalf("auth capabilities = %d, want %d", len(authPayload.Capabilities), len(messages.DefaultProtocolCapabilities()))
	}

	authOK, err := messages.New(messages.TypeAuthOK, messages.AuthOK{
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		t.Fatalf("New auth OK returned error: %v", err)
	}
	if err := wsjson.Write(ctx, conn, authOK); err != nil {
		t.Fatalf("write auth ok returned error: %v", err)
	}

	var registration messages.Envelope
	if err := wsjson.Read(ctx, conn, &registration); err != nil {
		t.Fatalf("read registration returned error: %v", err)
	}
	if registration.Type != messages.TypeTunnelRegister {
		t.Fatalf("registration type = %s, want %s", registration.Type, messages.TypeTunnelRegister)
	}
	registered, _ := messages.New(messages.TypeTunnelRegistered, messages.TunnelRegistered{
		TunnelID:  "tun_test",
		PublicURL: "http://demo.localhost:8080",
		Subdomain: "demo",
	})
	if err := wsjson.Write(ctx, conn, registered); err != nil {
		t.Fatalf("write registered returned error: %v", err)
	}
}
