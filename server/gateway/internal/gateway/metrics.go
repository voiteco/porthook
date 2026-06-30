// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type metrics struct {
	publicRequestsTotal        atomic.Uint64
	tokenValidationsTotal      atomic.Uint64
	tokenValidationErrorsTotal atomic.Uint64
	authFailuresTotal          atomic.Uint64
	tunnelRegistrationsTotal   atomic.Uint64
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writeMetric(w, "porthook_gateway_active_tunnels", "Current active tunnel sessions.", "gauge", uint64(s.registry.Count()))
	writeMetric(w, "porthook_gateway_public_requests_total", "Public HTTP requests handled by the gateway.", "counter", s.metrics.publicRequestsTotal.Load())
	writeMetric(w, "porthook_gateway_token_validations_total", "Agent token validation attempts.", "counter", s.metrics.tokenValidationsTotal.Load())
	writeMetric(w, "porthook_gateway_token_validation_errors_total", "Agent token validation attempts that failed before a valid/invalid result.", "counter", s.metrics.tokenValidationErrorsTotal.Load())
	writeMetric(w, "porthook_gateway_auth_failures_total", "Agent authentication failures.", "counter", s.metrics.authFailuresTotal.Load())
	writeMetric(w, "porthook_gateway_tunnel_registrations_total", "Successful tunnel registrations.", "counter", s.metrics.tunnelRegistrationsTotal.Load())
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func writeMetric(w http.ResponseWriter, name, help, metricType string, value uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
	fmt.Fprintf(w, "%s %d\n", name, value)
}
