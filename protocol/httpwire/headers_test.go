// SPDX-License-Identifier: Apache-2.0

package httpwire

import (
	"net/http"
	"testing"
)

func TestStripHopByHopHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("Connection", "X-Debug, keep-alive")
	header.Set("X-Debug", "remove")
	header.Set("Keep-Alive", "timeout=5")
	header.Set("Transfer-Encoding", "chunked")
	header.Set("Content-Type", "text/plain")

	cleaned := StripHopByHopHeaders(header)

	for _, name := range []string{"Connection", "X-Debug", "Keep-Alive", "Transfer-Encoding"} {
		if cleaned.Get(name) != "" {
			t.Fatalf("%s was not stripped", name)
		}
	}
	if cleaned.Get("Content-Type") != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", cleaned.Get("Content-Type"))
	}
}

func TestAddForwardedHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("Content-Type", "text/plain")

	forwarded := AddForwardedHeaders(header, "tun_123", "demo.localhost", "http", "127.0.0.1")

	if forwarded.Get("X-Porthook-Tunnel-ID") != "tun_123" {
		t.Fatalf("X-Porthook-Tunnel-ID = %q, want tun_123", forwarded.Get("X-Porthook-Tunnel-ID"))
	}
	if forwarded.Get("X-Forwarded-Host") != "demo.localhost" {
		t.Fatalf("X-Forwarded-Host = %q, want demo.localhost", forwarded.Get("X-Forwarded-Host"))
	}
	if forwarded.Get("X-Forwarded-Proto") != "http" {
		t.Fatalf("X-Forwarded-Proto = %q, want http", forwarded.Get("X-Forwarded-Proto"))
	}
	if forwarded.Get("X-Forwarded-For") != "127.0.0.1" {
		t.Fatalf("X-Forwarded-For = %q, want 127.0.0.1", forwarded.Get("X-Forwarded-For"))
	}
	if forwarded.Get("Content-Type") != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", forwarded.Get("Content-Type"))
	}
}
