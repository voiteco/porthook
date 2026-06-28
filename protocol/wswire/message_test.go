// SPDX-License-Identifier: Apache-2.0

package wswire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/voiteco/porthook/protocol/messages"
)

func TestReadAndWriteEnvelope(t *testing.T) {
	conn := testWebSocketPair(t)
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	env, err := messages.New(messages.TypePing, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := WriteEnvelope(ctx, conn, env); err != nil {
		t.Fatalf("WriteEnvelope returned error: %v", err)
	}

	got, err := Read(ctx, conn)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if got.Envelope.Type != messages.TypePing {
		t.Fatalf("type = %s, want %s", got.Envelope.Type, messages.TypePing)
	}
	if got.BinaryBody {
		t.Fatal("BinaryBody = true, want false")
	}
}

func TestReadAndWriteBinaryBody(t *testing.T) {
	conn := testWebSocketPair(t)
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := WriteBinaryBody(ctx, conn, messages.TypeHTTPRequestBody, "str_test", "tun_test", []byte("payload")); err != nil {
		t.Fatalf("WriteBinaryBody returned error: %v", err)
	}

	got, err := Read(ctx, conn)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if got.Envelope.Type != messages.TypeHTTPRequestBody {
		t.Fatalf("type = %s, want %s", got.Envelope.Type, messages.TypeHTTPRequestBody)
	}
	if got.Envelope.StreamID != "str_test" {
		t.Fatalf("stream id = %q, want str_test", got.Envelope.StreamID)
	}
	if got.Envelope.TunnelID != "tun_test" {
		t.Fatalf("tunnel id = %q, want tun_test", got.Envelope.TunnelID)
	}
	if !got.BinaryBody {
		t.Fatal("BinaryBody = false, want true")
	}
	if string(got.Body) != "payload" {
		t.Fatalf("body = %q, want payload", string(got.Body))
	}
}

func TestReadLimitForChunkBytes(t *testing.T) {
	got := ReadLimitForChunkBytes(32 << 10)
	want := int64(80 << 10)
	if got != want {
		t.Fatalf("read limit = %d, want %d", got, want)
	}
}

func testWebSocketPair(t *testing.T) *websocket.Conn {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept returned error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		for {
			messageType, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), messageType, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+server.URL[len("http"):], nil)
	if err != nil {
		t.Fatalf("Dial returned error: %v", err)
	}
	return conn
}
