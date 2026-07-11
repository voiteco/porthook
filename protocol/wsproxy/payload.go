// SPDX-License-Identifier: Apache-2.0

// Package wsproxy defines the payload types carried inside ws.open,
// ws.accept, and ws.close protocol envelopes when a public WebSocket
// upgrade is tunneled through to a local service. ws.error reuses
// messages.ErrorPayload and ws.cancel reuses messages.StreamCancel; the
// application data itself travels as WebSocket binary body frames (see
// protocol/messages/binary.go), not as envelope payloads.
package wsproxy

import "net/http"

// Open requests that the agent dial the local target's WebSocket endpoint
// at Path/Query, offering Subprotocols (may be empty) and Header. Header
// excludes hop-by-hop and WebSocket handshake fields (Connection, Upgrade,
// Sec-WebSocket-*), which the underlying WebSocket client and server handle
// themselves on each leg.
type Open struct {
	Path         string      `json:"path"`
	Query        string      `json:"query,omitempty"`
	Header       http.Header `json:"header,omitempty"`
	Subprotocols []string    `json:"subprotocols,omitempty"`
}

// Accept confirms the agent's local WebSocket dial succeeded, echoing the
// subprotocol the local service selected (empty if none).
type Accept struct {
	Subprotocol string `json:"subprotocol,omitempty"`
}

// Close conveys a graceful WebSocket close code and reason from one leg of
// a tunneled WebSocket connection to the other.
type Close struct {
	Code   int    `json:"code"`
	Reason string `json:"reason,omitempty"`
}
