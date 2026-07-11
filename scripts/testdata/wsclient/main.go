// SPDX-License-Identifier: AGPL-3.0-only

// Command wsclient is a minimal WebSocket test client used only by
// scripts/smoke-websocket.sh to drive a real public WebSocket upgrade
// through a real gateway and agent binary pair, proving the tunneled
// round trip actually works end to end. It connects, exchanges a text and
// a binary message with the echo service on the other end, verifies the
// negotiated subprotocol, and exits non-zero with a message on any
// mismatch.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"nhooyr.io/websocket"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: wsclient <ws-url> <host>")
		os.Exit(2)
	}
	url := os.Args[1]
	host := os.Args[2]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Host:         host,
		Subprotocols: []string{"echo.v1"},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != "echo.v1" {
		fmt.Fprintf(os.Stderr, "negotiated subprotocol = %q, want echo.v1\n", got)
		os.Exit(1)
	}

	if err := conn.Write(ctx, websocket.MessageText, []byte("porthook websocket smoke")); err != nil {
		fmt.Fprintf(os.Stderr, "write text failed: %v\n", err)
		os.Exit(1)
	}
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read text echo failed: %v\n", err)
		os.Exit(1)
	}
	if msgType != websocket.MessageText || string(data) != "porthook websocket smoke" {
		fmt.Fprintf(os.Stderr, "text echo = (%v, %q), want (Text, \"porthook websocket smoke\")\n", msgType, string(data))
		os.Exit(1)
	}

	binaryPayload := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0xFF}, 256)
	if err := conn.Write(ctx, websocket.MessageBinary, binaryPayload); err != nil {
		fmt.Fprintf(os.Stderr, "write binary failed: %v\n", err)
		os.Exit(1)
	}
	msgType, data, err = conn.Read(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read binary echo failed: %v\n", err)
		os.Exit(1)
	}
	if msgType != websocket.MessageBinary || !bytes.Equal(data, binaryPayload) {
		fmt.Fprintf(os.Stderr, "binary echo mismatch: got %d bytes, want %d bytes matching\n", len(data), len(binaryPayload))
		os.Exit(1)
	}

	if err := conn.Close(websocket.StatusNormalClosure, "smoke test done"); err != nil {
		fmt.Fprintf(os.Stderr, "close failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("websocket smoke check passed")
}
