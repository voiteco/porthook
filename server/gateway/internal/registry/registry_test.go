// SPDX-License-Identifier: AGPL-3.0-only

package registry

import (
	"testing"
	"time"
)

func TestRegistryRegisterLookupAndUnregister(t *testing.T) {
	registry := New()
	session := &Session{
		TunnelID:    "tun_123",
		Subdomain:   "demo",
		PublicURL:   "http://demo.localhost:8080",
		LocalTarget: "http://localhost:3000",
		Protocol:    "http",
		CreatedAt:   time.Now(),
	}

	if err := registry.Register(session); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if registry.Count() != 1 {
		t.Fatalf("Count = %d, want 1", registry.Count())
	}

	byID, ok := registry.LookupByTunnelID("tun_123")
	if !ok {
		t.Fatal("LookupByTunnelID returned ok=false")
	}
	if byID.Subdomain != "demo" {
		t.Fatalf("subdomain = %q, want demo", byID.Subdomain)
	}

	bySubdomain, ok := registry.LookupBySubdomain("demo")
	if !ok {
		t.Fatal("LookupBySubdomain returned ok=false")
	}
	if bySubdomain.TunnelID != "tun_123" {
		t.Fatalf("tunnel id = %q, want tun_123", bySubdomain.TunnelID)
	}

	registry.Unregister("tun_123")
	if registry.Count() != 0 {
		t.Fatalf("Count = %d, want 0", registry.Count())
	}
}

func TestSessionHasCapability(t *testing.T) {
	session := &Session{Capabilities: []string{"stream_start_end", "websocket_tunnel"}}
	if !session.HasCapability("websocket_tunnel") {
		t.Fatal("HasCapability(websocket_tunnel) = false, want true")
	}
	if session.HasCapability("missing") {
		t.Fatal("HasCapability(missing) = true, want false")
	}

	var nilSession *Session
	if nilSession.HasCapability("websocket_tunnel") {
		t.Fatal("nil session HasCapability = true, want false")
	}
}

func TestRegistryRejectsDuplicateSubdomain(t *testing.T) {
	registry := New()

	if err := registry.Register(&Session{TunnelID: "tun_1", Subdomain: "demo"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.Register(&Session{TunnelID: "tun_2", Subdomain: "demo"}); err == nil {
		t.Fatal("Register returned nil error for duplicate subdomain")
	}
}
