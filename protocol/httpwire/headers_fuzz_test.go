// SPDX-License-Identifier: Apache-2.0

package httpwire

import (
	"net/http"
	"testing"
)

func FuzzForwardedHeaders(f *testing.F) {
	f.Add("Upgrade, X-Remove", "X-Remove", "value", "tun_test", "demo.example.test", "https", "203.0.113.10")
	f.Add("keep-alive", "X-Test", "value", "", "", "", "")

	f.Fuzz(func(t *testing.T, connection, name, value, tunnelID, host, proto, remoteAddr string) {
		header := http.Header{
			"Connection": {connection},
			name:         {value},
		}
		cleaned := StripHopByHopHeaders(header)
		_ = AddForwardedHeaders(cleaned, tunnelID, host, proto, remoteAddr)
	})
}
