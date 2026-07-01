// SPDX-License-Identifier: AGPL-3.0-only

package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesDashboardIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/", nil)

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want html", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q, want self-only policy", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'self' http: https:") {
		t.Fatalf("Content-Security-Policy = %q, want gateway-capable connect-src", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Porthook") || !strings.Contains(body, "Token management") || !strings.Contains(body, "Custom domains") || !strings.Contains(body, "Access policies") || !strings.Contains(body, "Operational overview") || !strings.Contains(body, "Request logs") {
		t.Fatalf("dashboard index body = %q, want dashboard shell", body)
	}
}

func TestHandlerRedirectsDashboardRoot(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/dashboard/" {
		t.Fatalf("Location = %q, want /dashboard/", got)
	}
}

func TestHandlerServesAssets(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/app.js", nil)

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/api/v1/tokens") {
		t.Fatalf("asset body = %q, want token API client", body)
	}
	if !strings.Contains(body, "/api/v1/status") {
		t.Fatalf("asset body = %q, want status API client", body)
	}
	if !strings.Contains(body, "/api/v1/reserved-subdomains") {
		t.Fatalf("asset body = %q, want reserved subdomain API client", body)
	}
	if !strings.Contains(body, "/api/v1/custom-domains") {
		t.Fatalf("asset body = %q, want custom domain API client", body)
	}
	if !strings.Contains(body, "/api/v1/access-policies") {
		t.Fatalf("asset body = %q, want access policy API client", body)
	}
	if !strings.Contains(body, "/api/v1/tunnels") {
		t.Fatalf("asset body = %q, want gateway tunnel API client", body)
	}
	if !strings.Contains(body, "/api/v1/request-logs") {
		t.Fatalf("asset body = %q, want request logs API client", body)
	}
	if !strings.Contains(body, "renderOperationalOverview") {
		t.Fatalf("asset body = %q, want operational overview renderer", body)
	}
}
