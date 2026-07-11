// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/voiteco/porthook/server/control-plane/internal/admintokens"
)

const gatewayOperatorPrefix = "/api/v1/gateway"

func (s *Server) handleGatewayOperator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	upstreamPath, requiredScope, ok := gatewayOperatorRoute(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, ok := s.authorizeAdminRequest(w, r, requiredScope); !ok {
		return
	}
	if strings.TrimSpace(s.cfg.GatewayManagementURL) == "" || strings.TrimSpace(s.cfg.GatewayManagementToken) == "" {
		http.Error(w, "gateway management is not configured", http.StatusServiceUnavailable)
		return
	}

	baseURL, err := url.Parse(strings.TrimRight(s.cfg.GatewayManagementURL, "/") + upstreamPath)
	if err != nil {
		http.Error(w, "gateway management is not configured", http.StatusServiceUnavailable)
		return
	}
	baseURL.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, baseURL.String(), nil)
	if err != nil {
		http.Error(w, "create gateway management request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.GatewayManagementToken)
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}
	if requestID := r.Header.Get("X-Request-ID"); requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}

	resp, err := s.gatewayHTTPClient.Do(req)
	if err != nil {
		s.logger.WarnContext(r.Context(), "gateway management request failed",
			slog.String("event", "control_plane.gateway_management_failed"),
			slog.String("path", upstreamPath),
			slog.String("error", err.Error()),
		)
		http.Error(w, "gateway management request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyGatewayResponseHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		s.logger.WarnContext(r.Context(), "copy gateway management response failed",
			slog.String("event", "control_plane.gateway_management_response_failed"),
			slog.String("path", upstreamPath),
			slog.String("error", err.Error()),
		)
	}
}

func gatewayOperatorRoute(path string) (string, string, bool) {
	suffix := strings.TrimPrefix(path, gatewayOperatorPrefix)
	switch suffix {
	case "/healthz", "/readyz", "/metrics":
		return suffix, admintokens.ScopeRuntimeDiagnostics, true
	case "/runtime", "/tunnels":
		return "/api/v1" + suffix, admintokens.ScopeRuntimeDiagnostics, true
	case "/request-logs":
		return "/api/v1/request-logs", admintokens.ScopeAuditHistory, true
	default:
		tunnelID := strings.TrimPrefix(suffix, "/tunnels/")
		if strings.HasPrefix(suffix, "/tunnels/") && tunnelID != "" && !strings.Contains(tunnelID, "/") {
			return "/api/v1" + suffix, admintokens.ScopeRuntimeDiagnostics, true
		}
		return "", "", false
	}
}

func copyGatewayResponseHeader(dst, src http.Header) {
	for _, name := range []string{"Content-Type", "Cache-Control", "ETag", "Last-Modified"} {
		if value := src.Get(name); value != "" {
			dst.Set(name, value)
		}
	}
}
