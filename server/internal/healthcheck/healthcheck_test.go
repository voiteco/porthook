// SPDX-License-Identifier: AGPL-3.0-only

package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestURLForListenAddr(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		want       string
	}{
		{name: "wildcard", listenAddr: ":8080", want: "http://127.0.0.1:8080/readyz"},
		{name: "zero ipv4", listenAddr: "0.0.0.0:8080", want: "http://127.0.0.1:8080/readyz"},
		{name: "localhost", listenAddr: "localhost:8080", want: "http://localhost:8080/readyz"},
		{name: "ipv6 loopback", listenAddr: "[::1]:8080", want: "http://[::1]:8080/readyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := URLForListenAddr(tt.listenAddr, "/readyz")
			if err != nil {
				t.Fatalf("URLForListenAddr returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("URLForListenAddr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTTPReturnsStatusErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready: database unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := HTTP(context.Background(), server.URL, time.Second)
	if err == nil {
		t.Fatal("HTTP returned nil error")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("error = %q, want status and body", err.Error())
	}
}

func TestHTTPAcceptsSuccessfulStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ready\n"))
	}))
	defer server.Close()

	if err := HTTP(context.Background(), server.URL, time.Second); err != nil {
		t.Fatalf("HTTP returned error: %v", err)
	}
}

func TestURLFromEnvRejectsRelativeURL(t *testing.T) {
	t.Setenv("PORTHOOK_HEALTHCHECK_URL", "/readyz")

	_, err := URLFromEnvOrListenAddr(":8080", "/readyz")
	if err == nil {
		t.Fatal("URLFromEnvOrListenAddr returned nil error")
	}
	if !strings.Contains(err.Error(), "scheme and host are required") {
		t.Fatalf("error = %q, want scheme and host guidance", err.Error())
	}
}
