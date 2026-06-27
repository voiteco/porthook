// SPDX-License-Identifier: Apache-2.0

package httpwire

import (
	"net/http"
	"strings"
)

var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"TE":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func StripHopByHopHeaders(header http.Header) http.Header {
	cleaned := header.Clone()

	for _, value := range header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if token != "" {
				cleaned.Del(token)
			}
		}
	}

	for name := range hopByHopHeaders {
		cleaned.Del(name)
	}

	return cleaned
}

func AddForwardedHeaders(header http.Header, tunnelID, originalHost, proto, remoteAddr string) http.Header {
	forwarded := header.Clone()
	if remoteAddr != "" {
		forwarded.Set("X-Forwarded-For", remoteAddr)
	}
	if originalHost != "" {
		forwarded.Set("X-Forwarded-Host", originalHost)
	}
	if proto != "" {
		forwarded.Set("X-Forwarded-Proto", proto)
	}
	if tunnelID != "" {
		forwarded.Set("X-Porthook-Tunnel-ID", tunnelID)
	}
	return forwarded
}
