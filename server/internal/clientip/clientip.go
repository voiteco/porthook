// SPDX-License-Identifier: Apache-2.0

package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type Resolver struct {
	trusted []netip.Prefix
}

func New(trusted []netip.Prefix) Resolver {
	normalized := make([]netip.Prefix, 0, len(trusted))
	for _, prefix := range trusted {
		normalized = append(normalized, prefix.Masked())
	}
	return Resolver{trusted: normalized}
}

func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	prefixes := make([]netip.Prefix, 0, len(fields))
	for _, field := range fields {
		prefix, err := netip.ParsePrefix(field)
		if err != nil {
			addr, addrErr := netip.ParseAddr(field)
			if addrErr != nil {
				return nil, fmt.Errorf("parse trusted proxy %q: %w", field, err)
			}
			prefix = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen())
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func (r Resolver) Resolve(req *http.Request) netip.Addr {
	peer, ok := parseRemoteAddr(req.RemoteAddr)
	if !ok {
		return netip.Addr{}
	}
	if !r.isTrusted(peer) {
		return peer
	}

	forwarded, ok := forwardedChain(req.Header)
	if !ok || len(forwarded) == 0 {
		return peer
	}
	for i := len(forwarded) - 1; i >= 0; i-- {
		if !r.isTrusted(forwarded[i]) {
			return forwarded[i]
		}
	}
	return forwarded[0]
}

func (r Resolver) ResolveString(req *http.Request) string {
	addr := r.Resolve(req)
	if !addr.IsValid() {
		return ""
	}
	return addr.String()
}

func (r Resolver) isTrusted(addr netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func forwardedChain(header http.Header) ([]netip.Addr, bool) {
	values := header.Values("X-Forwarded-For")
	if len(values) == 0 {
		if value := strings.TrimSpace(header.Get("X-Real-IP")); value != "" {
			addr, err := netip.ParseAddr(value)
			if err != nil {
				return nil, false
			}
			return []netip.Addr{addr.Unmap()}, true
		}
		return nil, true
	}

	var chain []netip.Addr
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			addr, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil {
				return nil, false
			}
			chain = append(chain, addr.Unmap())
		}
	}
	return chain, true
}

func parseRemoteAddr(value string) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return addrPort.Addr().Unmap(), true
	}
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		value = host
	}
	addr, err := netip.ParseAddr(strings.Trim(value, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
