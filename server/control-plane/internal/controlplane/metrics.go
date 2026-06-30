// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type metrics struct {
	tokenAdminCreatesTotal      atomic.Uint64
	tokenAdminListsTotal        atomic.Uint64
	tokenAdminRevokesTotal      atomic.Uint64
	tokenValidationsTotal       atomic.Uint64
	tokenValidationValidTotal   atomic.Uint64
	tokenValidationInvalidTotal atomic.Uint64
	tokenValidationErrorsTotal  atomic.Uint64
	authFailuresTotal           atomic.Uint64
	readinessFailuresTotal      atomic.Uint64
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writeMetric(w, "porthook_control_plane_token_admin_creates_total", "Successful token create operations.", "counter", s.metrics.tokenAdminCreatesTotal.Load())
	writeMetric(w, "porthook_control_plane_token_admin_lists_total", "Successful token list operations.", "counter", s.metrics.tokenAdminListsTotal.Load())
	writeMetric(w, "porthook_control_plane_token_admin_revokes_total", "Successful token revoke operations.", "counter", s.metrics.tokenAdminRevokesTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validations_total", "Token validation requests handled by the control plane.", "counter", s.metrics.tokenValidationsTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validation_valid_total", "Token validation requests that returned valid=true.", "counter", s.metrics.tokenValidationValidTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validation_invalid_total", "Token validation requests that returned valid=false.", "counter", s.metrics.tokenValidationInvalidTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validation_errors_total", "Token validation requests that failed before producing a result.", "counter", s.metrics.tokenValidationErrorsTotal.Load())
	writeMetric(w, "porthook_control_plane_auth_failures_total", "Bearer authorization failures on control-plane endpoints.", "counter", s.metrics.authFailuresTotal.Load())
	writeMetric(w, "porthook_control_plane_readiness_failures_total", "Readiness checks that failed.", "counter", s.metrics.readinessFailuresTotal.Load())
}

func writeMetric(w http.ResponseWriter, name, help, metricType string, value uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
	fmt.Fprintf(w, "%s %d\n", name, value)
}
