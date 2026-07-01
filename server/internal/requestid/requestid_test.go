// SPDX-License-Identifier: AGPL-3.0-only

package requestid

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewarePreservesInboundRequestID(t *testing.T) {
	var got string
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = FromRequest(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(Header, "req_external")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if got != "req_external" {
		t.Fatalf("request id = %q, want req_external", got)
	}
	if resp.Header().Get(Header) != "req_external" {
		t.Fatalf("response header = %q, want req_external", resp.Header().Get(Header))
	}
}

func TestMiddlewareAcceptsCorrelationID(t *testing.T) {
	var got string
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = FromRequest(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Correlation-ID", "corr_external")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if got != "corr_external" {
		t.Fatalf("request id = %q, want corr_external", got)
	}
	if resp.Header().Get(Header) != "corr_external" {
		t.Fatalf("response header = %q, want corr_external", resp.Header().Get(Header))
	}
}

func TestMiddlewareGeneratesRequestID(t *testing.T) {
	var got string
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = FromRequest(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if !strings.HasPrefix(got, "req_") {
		t.Fatalf("request id = %q, want req_ prefix", got)
	}
	if resp.Header().Get(Header) != got {
		t.Fatalf("response header = %q, want %q", resp.Header().Get(Header), got)
	}
}

func TestMiddlewareRejectsInvalidInboundRequestID(t *testing.T) {
	var got string
	handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = FromRequest(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(Header, "bad id")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if got == "bad id" || !strings.HasPrefix(got, "req_") {
		t.Fatalf("request id = %q, want generated id", got)
	}
}
