// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type metrics struct {
	publicRequestsTotal               atomic.Uint64
	publicRequestErrorsTotal          atomic.Uint64
	publicRequestRateLimitedTotal     atomic.Uint64
	publicRequestTimeoutsTotal        atomic.Uint64
	publicRequestBodyTooLargeTotal    atomic.Uint64
	publicRequestClientCanceledTotal  atomic.Uint64
	publicRequestNoActiveSessionTotal atomic.Uint64
	tokenValidationsTotal             atomic.Uint64
	tokenValidationErrorsTotal        atomic.Uint64
	authFailuresTotal                 atomic.Uint64
	tunnelRegistrationsTotal          atomic.Uint64
	tunnelRegistrationFailuresTotal   atomic.Uint64
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writeMetric(w, "porthook_gateway_active_tunnels", "Current active tunnel sessions.", "gauge", uint64(s.registry.Count()))
	writeMetric(w, "porthook_gateway_uptime_seconds", "Gateway process uptime in seconds.", "gauge", uint64(time.Since(s.startedAt).Seconds()))
	writeMetric(w, "porthook_gateway_public_requests_total", "Public HTTP requests handled by the gateway.", "counter", s.metrics.publicRequestsTotal.Load())
	writeMetric(w, "porthook_gateway_public_request_errors_total", "Public HTTP requests that returned status >= 500.", "counter", s.metrics.publicRequestErrorsTotal.Load())
	writeMetric(w, "porthook_gateway_public_request_rate_limited_total", "Public HTTP requests rejected by per-tunnel rate limits.", "counter", s.metrics.publicRequestRateLimitedTotal.Load())
	writeMetric(w, "porthook_gateway_public_request_timeouts_total", "Public HTTP requests that timed out waiting for the tunnel.", "counter", s.metrics.publicRequestTimeoutsTotal.Load())
	writeMetric(w, "porthook_gateway_public_request_body_too_large_total", "Public HTTP requests rejected because the request body was too large.", "counter", s.metrics.publicRequestBodyTooLargeTotal.Load())
	writeMetric(w, "porthook_gateway_public_request_client_canceled_total", "Public HTTP requests canceled by the client before completion.", "counter", s.metrics.publicRequestClientCanceledTotal.Load())
	writeMetric(w, "porthook_gateway_public_request_no_active_session_total", "Public HTTP requests for hosts without an active tunnel session.", "counter", s.metrics.publicRequestNoActiveSessionTotal.Load())
	writeMetric(w, "porthook_gateway_token_validations_total", "Agent token validation attempts.", "counter", s.metrics.tokenValidationsTotal.Load())
	writeMetric(w, "porthook_gateway_token_validation_errors_total", "Agent token validation attempts that failed before a valid/invalid result.", "counter", s.metrics.tokenValidationErrorsTotal.Load())
	writeMetric(w, "porthook_gateway_auth_failures_total", "Agent authentication failures.", "counter", s.metrics.authFailuresTotal.Load())
	writeMetric(w, "porthook_gateway_tunnel_registrations_total", "Successful tunnel registrations.", "counter", s.metrics.tunnelRegistrationsTotal.Load())
	writeMetric(w, "porthook_gateway_tunnel_registration_failures_total", "Tunnel registration attempts that failed after agent authentication.", "counter", s.metrics.tunnelRegistrationFailuresTotal.Load())
}

func (s *Server) recordPublicRequestMetrics(status int, outcome string) {
	s.metrics.publicRequestsTotal.Add(1)
	if status >= 500 {
		s.metrics.publicRequestErrorsTotal.Add(1)
	}
	switch outcome {
	case "rate_limited":
		s.metrics.publicRequestRateLimitedTotal.Add(1)
	case "tunnel_timeout":
		s.metrics.publicRequestTimeoutsTotal.Add(1)
	case "request_body_too_large":
		s.metrics.publicRequestBodyTooLargeTotal.Add(1)
	case "client_canceled":
		s.metrics.publicRequestClientCanceledTotal.Add(1)
	case "no_active_session", "no_tunnel_for_host":
		s.metrics.publicRequestNoActiveSessionTotal.Add(1)
	}
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
