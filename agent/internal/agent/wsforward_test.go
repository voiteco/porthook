// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/wsproxy"
	"github.com/voiteco/porthook/protocol/wswire"
)

func TestRunnerRelaysWebSocketTextAndBinaryMessages(t *testing.T) {
	var gotSubprotocols []string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSubprotocols = r.Header["Sec-Websocket-Protocol"]
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{"chat.v1"},
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("local Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		for i := 0; i < 2; i++ {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, msgType, data); err != nil {
				return
			}
		}
	}))
	defer local.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("gateway Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		readAuthAndRegistration(t, ctx, conn)

		open, err := messages.NewStream(messages.TypeWSOpen, "str_ws", "tun_test", wsproxy.Open{
			Path:         "/socket",
			Subprotocols: []string{"chat.v1"},
		})
		if err != nil {
			t.Errorf("NewStream open returned error: %v", err)
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, open); err != nil {
			t.Errorf("write ws open returned error: %v", err)
			return
		}

		acceptMsg, err := wswire.Read(ctx, conn)
		if err != nil {
			t.Errorf("read ws accept returned error: %v", err)
			return
		}
		if acceptMsg.Envelope.Type != messages.TypeWSAccept {
			t.Errorf("response type = %s, want %s", acceptMsg.Envelope.Type, messages.TypeWSAccept)
			return
		}
		accept, err := messages.DecodePayload[wsproxy.Accept](acceptMsg.Envelope)
		if err != nil {
			t.Errorf("DecodePayload accept returned error: %v", err)
			return
		}
		if accept.Subprotocol != "chat.v1" {
			t.Errorf("subprotocol = %q, want chat.v1", accept.Subprotocol)
		}

		if err := wswire.WriteBinaryBody(ctx, conn, messages.TypeWSMessageText, "str_ws", "tun_test", []byte("hello")); err != nil {
			t.Errorf("write ws text message returned error: %v", err)
			return
		}
		echoText, err := wswire.Read(ctx, conn)
		if err != nil {
			t.Errorf("read ws text echo returned error: %v", err)
			return
		}
		if echoText.Envelope.Type != messages.TypeWSMessageText || string(echoText.Body) != "hello" {
			t.Errorf("text echo = (%s, %q), want (%s, hello)", echoText.Envelope.Type, string(echoText.Body), messages.TypeWSMessageText)
		}

		binaryPayload := []byte{0x01, 0x02, 0x03}
		if err := wswire.WriteBinaryBody(ctx, conn, messages.TypeWSMessageBinary, "str_ws", "tun_test", binaryPayload); err != nil {
			t.Errorf("write ws binary message returned error: %v", err)
			return
		}
		echoBinary, err := wswire.Read(ctx, conn)
		if err != nil {
			t.Errorf("read ws binary echo returned error: %v", err)
			return
		}
		if echoBinary.Envelope.Type != messages.TypeWSMessageBinary || !bytes.Equal(echoBinary.Body, binaryPayload) {
			t.Errorf("binary echo = (%s, %x), want (%s, %x)", echoBinary.Envelope.Type, echoBinary.Body, messages.TypeWSMessageBinary, binaryPayload)
		}

		closeEnv, err := messages.NewStream(messages.TypeWSClose, "str_ws", "tun_test", wsproxy.Close{Code: 1000})
		if err != nil {
			t.Errorf("NewStream close returned error: %v", err)
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, closeEnv); err != nil {
			t.Errorf("write ws close returned error: %v", err)
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
	if len(gotSubprotocols) != 1 || gotSubprotocols[0] != "chat.v1" {
		t.Fatalf("local dial subprotocols = %v, want [chat.v1]", gotSubprotocols)
	}
}

func TestRunnerSendsWSErrorWhenLocalDialFails(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentWebSocketPath {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("gateway Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		readAuthAndRegistration(t, ctx, conn)

		open, err := messages.NewStream(messages.TypeWSOpen, "str_ws", "tun_test", wsproxy.Open{Path: "/socket"})
		if err != nil {
			t.Errorf("NewStream open returned error: %v", err)
			return
		}
		if err := wswire.WriteEnvelope(ctx, conn, open); err != nil {
			t.Errorf("write ws open returned error: %v", err)
			return
		}

		errMsg, err := wswire.Read(ctx, conn)
		if err != nil {
			t.Errorf("read ws error returned error: %v", err)
			return
		}
		if errMsg.Envelope.Type != messages.TypeWSError {
			t.Errorf("response type = %s, want %s", errMsg.Envelope.Type, messages.TypeWSError)
			return
		}
		payload, err := messages.DecodePayload[messages.ErrorPayload](errMsg.Envelope)
		if err != nil {
			t.Errorf("DecodePayload error returned error: %v", err)
			return
		}
		if payload.Code != "local_dial_failed" {
			t.Errorf("error code = %q, want local_dial_failed", payload.Code)
		}
	}))
	defer gateway.Close()

	var output bytes.Buffer
	runner := NewRunner(Config{
		ServerURL:          gateway.URL,
		Token:              "dev-token",
		RequestedSubdomain: "demo",
		LocalTarget:        "http://127.0.0.1:1", // nothing listens here
		AgentVersion:       "test",
		RequestTimeout:     time.Second,
	}, nil, &output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}
