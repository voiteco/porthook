// SPDX-License-Identifier: AGPL-3.0-only

// Command wsecho is a minimal local WebSocket echo server used only by
// scripts/smoke-websocket.sh to prove a real agent binary can tunnel real
// public WebSocket traffic to a real local WebSocket service. It echoes
// every message back verbatim, preserving text/binary framing.
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
