// SPDX-License-Identifier: Apache-2.0

package clientip

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestResolverUsesDirectPeerWithoutTrustedProxies(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.test", nil)
	req.RemoteAddr = "192.0.2.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.8")

	if got := New(nil).ResolveString(req); got != "192.0.2.10" {
		t.Fatalf("ResolveString = %q, want direct peer", got)
	}
}

func TestResolverSelectsFirstUntrustedAddressFromRight(t *testing.T) {
	resolver := New([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
	})
	req := httptest.NewRequest("GET", "http://example.test", nil)
	req.RemoteAddr = "10.0.0.4:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.7, 172.16.0.2")

	if got := resolver.ResolveString(req); got != "203.0.113.7" {
		t.Fatalf("ResolveString = %q, want first untrusted address", got)
	}
}

func TestResolverSupportsIPv6ProxyChains(t *testing.T) {
	resolver := New([]netip.Prefix{netip.MustParsePrefix("2001:db8:1::/48")})
	req := httptest.NewRequest("GET", "http://example.test", nil)
	req.RemoteAddr = "[2001:db8:1::10]:443"
	req.Header.Set("X-Forwarded-For", "2001:db8:ffff::20, 2001:db8:1::11")

	if got := resolver.ResolveString(req); got != "2001:db8:ffff::20" {
		t.Fatalf("ResolveString = %q, want IPv6 client", got)
	}
}

func TestResolverUsesRealIPFromTrustedPeer(t *testing.T) {
	resolver := New([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	req := httptest.NewRequest("GET", "http://example.test", nil)
	req.RemoteAddr = "127.0.0.1:443"
	req.Header.Set("X-Real-IP", "198.51.100.12")

	if got := resolver.ResolveString(req); got != "198.51.100.12" {
		t.Fatalf("ResolveString = %q, want X-Real-IP client", got)
	}
}

func TestResolverRejectsMalformedForwardedChain(t *testing.T) {
	resolver := New([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	req := httptest.NewRequest("GET", "http://example.test", nil)
	req.RemoteAddr = "10.0.0.4:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.12, invalid")

	if got := resolver.ResolveString(req); got != "10.0.0.4" {
		t.Fatalf("ResolveString = %q, want trusted peer fallback", got)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	prefixes, err := ParseTrustedProxies("10.0.0.0/8, 192.0.2.10 2001:db8::/32")
	if err != nil {
		t.Fatalf("ParseTrustedProxies returned error: %v", err)
	}
	if len(prefixes) != 3 {
		t.Fatalf("len(prefixes) = %d, want 3", len(prefixes))
	}
	if got := prefixes[1].String(); got != "192.0.2.10/32" {
		t.Fatalf("single address prefix = %q, want /32", got)
	}
}

func TestParseTrustedProxiesRejectsInvalidValue(t *testing.T) {
	if _, err := ParseTrustedProxies("not-a-network"); err == nil {
		t.Fatal("ParseTrustedProxies returned nil error")
	}
}
