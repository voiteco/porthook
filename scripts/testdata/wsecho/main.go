// SPDX-License-Identifier: AGPL-3.0-only

// Command wsecho is a minimal local HTTP+WebSocket service used by
// scripts/smoke-websocket.sh and scripts/smoke-tls-edge.sh to prove a real
// agent binary can tunnel real public HTTP and WebSocket traffic to a real
// local service. GET /smoke.txt returns a fixed marker body; /socket echoes
// every WebSocket message back verbatim, preserving text/binary framing.
package main

import (
	"log"
	"net/http"
	"os"

	"nhooyr.io/websocket"
)

func main() {
	addr := os.Getenv("WSECHO_ADDR")
	if addr == "" {
		addr = "127.0.0.1:13100"
	}

	http.HandleFunc("/smoke.txt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		// Echo back what the edge and tunnel actually forwarded, so a smoke
		// test can assert on X-Forwarded-*, Host, and other request shape
		// from the response alone, without needing local server logs.
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Echo-Host", r.Host)
		w.Header().Set("X-Echo-Forwarded-For", r.Header.Get("X-Forwarded-For"))
		w.Header().Set("X-Echo-Forwarded-Host", r.Header.Get("X-Forwarded-Host"))
		w.Header().Set("X-Echo-Forwarded-Proto", r.Header.Get("X-Forwarded-Proto"))
		_, _ = w.Write([]byte("porthook wsecho smoke ok\n"))
	})

	http.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{"echo.v1"},
			InsecureSkipVerify: true,
		})
		if err != nil {
			log.Printf("wsecho: accept failed: %v", err)
			return
		}
		defer conn.CloseNow()

		ctx := r.Context()
		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, msgType, data); err != nil {
				return
			}
		}
	})

	log.Printf("wsecho: listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("wsecho: %v", err)
	}
}
