// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	porthookmetrics "github.com/voiteco/porthook/server/internal/metrics"
)

type metrics struct {
	tokenAdminCreatesTotal               atomic.Uint64
	tokenAdminListsTotal                 atomic.Uint64
	tokenAdminRevokesTotal               atomic.Uint64
	tokenValidationsTotal                atomic.Uint64
	tokenValidationValidTotal            atomic.Uint64
	tokenValidationInvalidTotal          atomic.Uint64
	tokenValidationErrorsTotal           atomic.Uint64
	reservationAdminCreatesTotal         atomic.Uint64
	reservationAdminListsTotal           atomic.Uint64
	reservationAdminDeletesTotal         atomic.Uint64
	reservationAuthorizationsTotal       atomic.Uint64
	reservationAuthorizationAllowedTotal atomic.Uint64
	reservationAuthorizationDeniedTotal  atomic.Uint64
	accessPolicyAdminCreatesTotal        atomic.Uint64
	accessPolicyAdminListsTotal          atomic.Uint64
	accessPolicyAdminUpdatesTotal        atomic.Uint64
	accessPolicyAdminDeletesTotal        atomic.Uint64
	accessPolicyEvaluationsTotal         atomic.Uint64
	accessPolicyEvaluationAllowedTotal   atomic.Uint64
	accessPolicyEvaluationDeniedTotal    atomic.Uint64
	accessPolicyEvaluationErrorsTotal    atomic.Uint64
	customDomainAdminCreatesTotal        atomic.Uint64
	customDomainAdminListsTotal          atomic.Uint64
	customDomainAdminDeletesTotal        atomic.Uint64
	customDomainLookupsTotal             atomic.Uint64
	customDomainLookupHitsTotal          atomic.Uint64
	customDomainLookupMissesTotal        atomic.Uint64
	customDomainLookupErrorsTotal        atomic.Uint64
	authFailuresTotal                    atomic.Uint64
	readinessFailuresTotal               atomic.Uint64
	requestDuration                      *porthookmetrics.Histogram
}

func newMetrics() metrics {
	return metrics{requestDuration: porthookmetrics.NewHistogram(porthookmetrics.DefaultLatencyBuckets)}
}

// latencyMiddleware records how long next takes to handle every request,
// regardless of which endpoint served it.
func (s *Server) latencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.metrics.requestDuration.Observe(time.Since(started))
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writeMetric(w, "porthook_control_plane_ready", "Control-plane readiness state, 1 for ready and 0 for not ready.", "gauge", s.readinessGauge(r))
	writeMetric(w, "porthook_control_plane_uptime_seconds", "Control-plane process uptime in seconds.", "gauge", uint64(time.Since(s.startedAt).Seconds()))
	s.writeInventoryMetrics(w, r)
	writeMetric(w, "porthook_control_plane_token_admin_creates_total", "Successful token create operations.", "counter", s.metrics.tokenAdminCreatesTotal.Load())
	writeMetric(w, "porthook_control_plane_token_admin_lists_total", "Successful token list operations.", "counter", s.metrics.tokenAdminListsTotal.Load())
	writeMetric(w, "porthook_control_plane_token_admin_revokes_total", "Successful token revoke operations.", "counter", s.metrics.tokenAdminRevokesTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validations_total", "Token validation requests handled by the control plane.", "counter", s.metrics.tokenValidationsTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validation_valid_total", "Token validation requests that returned valid=true.", "counter", s.metrics.tokenValidationValidTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validation_invalid_total", "Token validation requests that returned valid=false.", "counter", s.metrics.tokenValidationInvalidTotal.Load())
	writeMetric(w, "porthook_control_plane_token_validation_errors_total", "Token validation requests that failed before producing a result.", "counter", s.metrics.tokenValidationErrorsTotal.Load())
	writeMetric(w, "porthook_control_plane_reservation_admin_creates_total", "Successful reserved subdomain create operations.", "counter", s.metrics.reservationAdminCreatesTotal.Load())
	writeMetric(w, "porthook_control_plane_reservation_admin_lists_total", "Successful reserved subdomain list operations.", "counter", s.metrics.reservationAdminListsTotal.Load())
	writeMetric(w, "porthook_control_plane_reservation_admin_deletes_total", "Successful reserved subdomain delete operations.", "counter", s.metrics.reservationAdminDeletesTotal.Load())
	writeMetric(w, "porthook_control_plane_reservation_authorizations_total", "Reserved subdomain authorization requests handled by the control plane.", "counter", s.metrics.reservationAuthorizationsTotal.Load())
	writeMetric(w, "porthook_control_plane_reservation_authorization_allowed_total", "Reserved subdomain authorization requests that returned allowed=true.", "counter", s.metrics.reservationAuthorizationAllowedTotal.Load())
	writeMetric(w, "porthook_control_plane_reservation_authorization_denied_total", "Reserved subdomain authorization requests that returned allowed=false.", "counter", s.metrics.reservationAuthorizationDeniedTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_admin_creates_total", "Successful access policy create operations.", "counter", s.metrics.accessPolicyAdminCreatesTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_admin_lists_total", "Successful access policy list operations.", "counter", s.metrics.accessPolicyAdminListsTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_admin_updates_total", "Successful access policy update operations.", "counter", s.metrics.accessPolicyAdminUpdatesTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_admin_deletes_total", "Successful access policy delete operations.", "counter", s.metrics.accessPolicyAdminDeletesTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_evaluations_total", "Access policy evaluation requests handled by the control plane.", "counter", s.metrics.accessPolicyEvaluationsTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_evaluation_allowed_total", "Access policy evaluation requests that returned allowed=true.", "counter", s.metrics.accessPolicyEvaluationAllowedTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_evaluation_denied_total", "Access policy evaluation requests that returned allowed=false.", "counter", s.metrics.accessPolicyEvaluationDeniedTotal.Load())
	writeMetric(w, "porthook_control_plane_access_policy_evaluation_errors_total", "Access policy evaluation requests that failed before producing a result.", "counter", s.metrics.accessPolicyEvaluationErrorsTotal.Load())
	writeMetric(w, "porthook_control_plane_custom_domain_admin_creates_total", "Successful custom domain create operations.", "counter", s.metrics.customDomainAdminCreatesTotal.Load())
	writeMetric(w, "porthook_control_plane_custom_domain_admin_lists_total", "Successful custom domain list operations.", "counter", s.metrics.customDomainAdminListsTotal.Load())
	writeMetric(w, "porthook_control_plane_custom_domain_admin_deletes_total", "Successful custom domain delete operations.", "counter", s.metrics.customDomainAdminDeletesTotal.Load())
	writeMetric(w, "porthook_control_plane_custom_domain_lookups_total", "Custom domain lookup requests handled by the control plane.", "counter", s.metrics.customDomainLookupsTotal.Load())
	writeMetric(w, "porthook_control_plane_custom_domain_lookup_hits_total", "Custom domain lookup requests that found a domain mapping.", "counter", s.metrics.customDomainLookupHitsTotal.Load())
	writeMetric(w, "porthook_control_plane_custom_domain_lookup_misses_total", "Custom domain lookup requests that did not find a domain mapping.", "counter", s.metrics.customDomainLookupMissesTotal.Load())
	writeMetric(w, "porthook_control_plane_custom_domain_lookup_errors_total", "Custom domain lookup requests that failed before producing a result.", "counter", s.metrics.customDomainLookupErrorsTotal.Load())
	writeMetric(w, "porthook_control_plane_auth_failures_total", "Bearer authorization failures on control-plane endpoints.", "counter", s.metrics.authFailuresTotal.Load())
	writeMetric(w, "porthook_control_plane_readiness_failures_total", "Readiness checks that failed.", "counter", s.metrics.readinessFailuresTotal.Load())

	s.metrics.requestDuration.WriteTo(w, "porthook_control_plane_request_duration_seconds", "HTTP request duration in seconds.")
	porthookmetrics.WriteRuntimeStats(w, "porthook_control_plane")
	if statser, ok := s.auditEvents.(dbStatser); ok {
		porthookmetrics.WriteDBPoolStats(w, "porthook_control_plane", statser.Stats())
	}
}

// dbStatser is implemented by stores backed by a real connection pool
// (currently only *PostgresAuditEventStore, which shares its *sql.DB with
// every other Postgres store the control plane uses).
type dbStatser interface {
	Stats() sql.DBStats
}

func (s *Server) readinessGauge(r *http.Request) uint64 {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		return 0
	}
	return 1
}

func (s *Server) writeInventoryMetrics(w http.ResponseWriter, r *http.Request) {
	listedTokens, err := s.service.ListTokens(r.Context())
	if err == nil {
		revoked := 0
		for _, token := range listedTokens.Tokens {
			if token.RevokedAt != nil {
				revoked++
			}
		}
		writeMetric(w, "porthook_control_plane_tokens", "Current token records in the control plane.", "gauge", uint64(len(listedTokens.Tokens)))
		writeMetric(w, "porthook_control_plane_tokens_revoked", "Current revoked token records in the control plane.", "gauge", uint64(revoked))
	}

	listedReservations, err := s.reservations.ListReservations(r.Context())
	if err == nil {
		writeMetric(w, "porthook_control_plane_reserved_subdomains", "Current reserved subdomain records in the control plane.", "gauge", uint64(len(listedReservations.ReservedSubdomains)))
	}

	listedAccessPolicies, err := s.accessPolicies.ListPolicies(r.Context())
	if err == nil {
		writeMetric(w, "porthook_control_plane_access_policies", "Current access policy records in the control plane.", "gauge", uint64(len(listedAccessPolicies.AccessPolicies)))
	}

	listedCustomDomains, err := s.customDomains.ListDomains(r.Context())
	if err == nil {
		writeMetric(w, "porthook_control_plane_custom_domains", "Current custom domain records in the control plane.", "gauge", uint64(len(listedCustomDomains.CustomDomains)))
	}
}

func writeMetric(w http.ResponseWriter, name, help, metricType string, value uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
	fmt.Fprintf(w, "%s %d\n", name, value)
}
