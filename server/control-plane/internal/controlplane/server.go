// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/voiteco/porthook/server/control-plane/internal/access"
	"github.com/voiteco/porthook/server/control-plane/internal/customdomains"
	"github.com/voiteco/porthook/server/control-plane/internal/reserved"
	"github.com/voiteco/porthook/server/control-plane/internal/tokens"
	"github.com/voiteco/porthook/server/dashboard"
	"github.com/voiteco/porthook/server/internal/requestid"
	"github.com/voiteco/porthook/server/internal/telemetry"
)

type Server struct {
	cfg            Config
	service        *tokens.Service
	reservations   *reserved.Service
	accessPolicies *access.Service
	customDomains  *customdomains.Service
	logger         *slog.Logger
	metrics        metrics
	startedAt      time.Time
	auditEvents    *auditEventBuffer
}

type validateTokenRequest struct {
	Token string `json:"token"`
	Scope string `json:"scope,omitempty"`
}

type authorizeReservedSubdomainRequest struct {
	TokenID   string `json:"token_id"`
	Subdomain string `json:"subdomain"`
}

type evaluateAccessPolicyRequest struct {
	Subdomain     string `json:"subdomain"`
	RemoteIP      string `json:"remote_ip,omitempty"`
	BasicUsername string `json:"basic_username,omitempty"`
	BasicPassword string `json:"basic_password,omitempty"`
	BearerToken   string `json:"bearer_token,omitempty"`
}

type evaluateAccessPolicyResponse struct {
	Allowed bool              `json:"allowed"`
	Mode    access.PolicyMode `json:"mode"`
	Reason  string            `json:"reason,omitempty"`
}

type lookupCustomDomainRequest struct {
	Hostname string `json:"hostname"`
}

type lookupCustomDomainResponse struct {
	Found               bool                       `json:"found"`
	Reason              string                     `json:"reason,omitempty"`
	Hostname            string                     `json:"hostname,omitempty"`
	CustomDomainID      string                     `json:"custom_domain_id,omitempty"`
	ReservedSubdomainID string                     `json:"reserved_subdomain_id,omitempty"`
	Subdomain           string                     `json:"subdomain,omitempty"`
	Status              customdomains.DomainStatus `json:"status,omitempty"`
}

type statusResponse struct {
	Status  string `json:"status"`
	Ready   bool   `json:"ready"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

const maxJSONBodyBytes = 1 << 20

const readinessTimeout = 2 * time.Second
const defaultAuditEventLimit = 500

func NewServer(cfg Config, service *tokens.Service, reservationServices ...*reserved.Service) *Server {
	reservationService := reserved.NewService(reserved.NewMemoryStore())
	if len(reservationServices) > 0 && reservationServices[0] != nil {
		reservationService = reservationServices[0]
	}
	return NewServerWithAccessPolicies(cfg, service, reservationService, nil)
}

func NewServerWithAccessPolicies(cfg Config, service *tokens.Service, reservationService *reserved.Service, accessPolicyService *access.Service) *Server {
	return NewServerWithCustomDomains(cfg, service, reservationService, accessPolicyService, nil)
}

func NewServerWithCustomDomains(cfg Config, service *tokens.Service, reservationService *reserved.Service, accessPolicyService *access.Service, customDomainService *customdomains.Service) *Server {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if service == nil {
		service = tokens.NewService(tokens.NewMemoryStore())
	}
	if reservationService == nil {
		reservationService = reserved.NewService(reserved.NewMemoryStore())
	}
	if accessPolicyService == nil {
		accessPolicyService = access.NewService(access.NewMemoryStore())
	}
	if customDomainService == nil {
		customDomainService = customdomains.NewService(customdomains.NewMemoryStore())
	}
	return &Server{
		cfg:            cfg,
		service:        service,
		reservations:   reservationService,
		accessPolicies: accessPolicyService,
		customDomains:  customDomainService,
		logger:         slog.Default(),
		startedAt:      time.Now().UTC(),
		auditEvents:    newAuditEventBuffer(defaultAuditEventLimit),
	}
}

func (s *Server) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	dashboardHandler := dashboard.Handler()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", s.handleReady)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.Handle("/dashboard", dashboardHandler)
	mux.Handle("/dashboard/", dashboardHandler)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/tokens", s.handleTokens)
	mux.HandleFunc("/api/v1/tokens/", s.handleTokenByID)
	mux.HandleFunc("/api/v1/tokens/validate", s.handleValidateToken)
	mux.HandleFunc("/api/v1/access-policies", s.handleAccessPolicies)
	mux.HandleFunc("/api/v1/access-policies/", s.handleAccessPolicyByID)
	mux.HandleFunc("/api/v1/access-policies/evaluate", s.handleEvaluateAccessPolicy)
	mux.HandleFunc("/api/v1/custom-domains", s.handleCustomDomains)
	mux.HandleFunc("/api/v1/custom-domains/", s.handleCustomDomainByID)
	mux.HandleFunc("/api/v1/custom-domains/lookup", s.handleLookupCustomDomain)
	mux.HandleFunc("/api/v1/reserved-subdomains", s.handleReservedSubdomains)
	mux.HandleFunc("/api/v1/reserved-subdomains/", s.handleReservedSubdomainByID)
	mux.HandleFunc("/api/v1/reserved-subdomains/authorize", s.handleAuthorizeReservedSubdomain)
	return requestid.Middleware(telemetry.HTTPHandler(mux, "control_plane.http"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		s.metrics.readinessFailuresTotal.Add(1)
		http.Error(w, "not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	resp := statusResponse{
		Status:  "ready",
		Ready:   true,
		Version: s.cfg.Version,
	}
	if err := s.ready(ctx); err != nil {
		resp.Status = "not_ready"
		resp.Ready = false
		resp.Error = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	limit := auditEventLimit(r, defaultAuditEventLimit)
	events := s.auditEvents.list(limit)
	s.logAudit(r, slog.LevelInfo, "control-plane audit events listed", "control_plane.audit_events_listed", slog.Int("count", len(events)))
	writeJSON(w, http.StatusOK, auditEventsResponse{Events: events})
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/tokens" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, POST")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		listed, err := s.service.ListTokens(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.metrics.tokenAdminListsTotal.Add(1)
		s.logAudit(r, slog.LevelInfo, "control-plane tokens listed", "control_plane.tokens_listed", slog.Int("count", len(listed.Tokens)))
		writeJSON(w, http.StatusOK, listed)
		return
	}

	var req tokens.CreateTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	created, err := s.service.CreateToken(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if !isTokenRequestError(err) {
			status = http.StatusInternalServerError
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.metrics.tokenAdminCreatesTotal.Add(1)
	s.logAudit(r, slog.LevelInfo, "control-plane token created", "control_plane.token_created",
		slog.String("token_id", created.ID),
		slog.String("name", created.Name),
		slog.Any("scopes", created.Scopes),
	)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleValidateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.validatorAuthorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "validator"),
			slog.Bool("token_configured", s.cfg.ValidatorToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req validateTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.metrics.tokenValidationsTotal.Add(1)
	result, err := s.service.ValidateToken(r.Context(), req.Token, req.Scope)
	if err != nil {
		s.metrics.tokenValidationErrorsTotal.Add(1)
		status := http.StatusInternalServerError
		if errors.Is(err, tokens.ErrUnsupportedScope) {
			status = http.StatusBadRequest
		}
		s.logAudit(r, slog.LevelWarn, "control-plane token validation failed", "control_plane.token_validation_failed",
			slog.String("scope", req.Scope),
			slog.Int("status", status),
			slog.Any("error", err),
		)
		http.Error(w, err.Error(), status)
		return
	}
	if result.Valid {
		s.metrics.tokenValidationValidTotal.Add(1)
	} else {
		s.metrics.tokenValidationInvalidTotal.Add(1)
	}
	attrs := []slog.Attr{
		slog.String("scope", req.Scope),
		slog.Bool("valid", result.Valid),
	}
	if result.TokenID != "" {
		attrs = append(attrs, slog.String("token_id", result.TokenID))
	}
	s.logAudit(r, slog.LevelInfo, "control-plane token validated", "control_plane.token_validated", attrs...)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleReservedSubdomains(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/reserved-subdomains" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, POST")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		listed, err := s.reservations.ListReservations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.metrics.reservationAdminListsTotal.Add(1)
		s.logAudit(r, slog.LevelInfo, "control-plane reserved subdomains listed", "control_plane.reservations_listed", slog.Int("count", len(listed.ReservedSubdomains)))
		writeJSON(w, http.StatusOK, listed)
		return
	}

	var req reserved.CreateReservationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token, ok, err := s.service.GetToken(r.Context(), req.TokenID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "token_id was not found", http.StatusBadRequest)
		return
	}
	if token.RevokedAt != nil {
		http.Error(w, "token_id belongs to a revoked token", http.StatusBadRequest)
		return
	}

	created, err := s.reservations.CreateReservation(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, reserved.ErrReservationAlreadyExists):
			status = http.StatusConflict
		case isReservationRequestError(err):
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.metrics.reservationAdminCreatesTotal.Add(1)
	s.logAudit(r, slog.LevelInfo, "control-plane reserved subdomain created", "control_plane.reservation_created",
		slog.String("reservation_id", created.ID),
		slog.String("subdomain", created.Name),
		slog.String("token_id", created.TokenID),
	)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleAuthorizeReservedSubdomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.validatorAuthorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "validator"),
			slog.Bool("token_configured", s.cfg.ValidatorToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req authorizeReservedSubdomainRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.metrics.reservationAuthorizationsTotal.Add(1)
	result, err := s.reservations.Authorize(r.Context(), req.TokenID, req.Subdomain)
	if err != nil {
		status := http.StatusInternalServerError
		if isReservationRequestError(err) {
			status = http.StatusBadRequest
		}
		s.logAudit(r, slog.LevelWarn, "control-plane reserved subdomain authorization failed", "control_plane.reservation_authorization_failed",
			slog.String("token_id", req.TokenID),
			slog.String("subdomain", req.Subdomain),
			slog.Int("status", status),
			slog.Any("error", err),
		)
		http.Error(w, err.Error(), status)
		return
	}
	if result.Allowed {
		s.metrics.reservationAuthorizationAllowedTotal.Add(1)
	} else {
		s.metrics.reservationAuthorizationDeniedTotal.Add(1)
	}
	s.logAudit(r, slog.LevelInfo, "control-plane reserved subdomain authorized", "control_plane.reservation_authorized",
		slog.String("token_id", req.TokenID),
		slog.String("subdomain", req.Subdomain),
		slog.Bool("allowed", result.Allowed),
		slog.String("reason", result.Reason),
	)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAccessPolicies(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/access-policies" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, POST")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		listed, err := s.accessPolicies.ListPolicies(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.metrics.accessPolicyAdminListsTotal.Add(1)
		s.logAudit(r, slog.LevelInfo, "control-plane access policies listed", "control_plane.access_policies_listed", slog.Int("count", len(listed.AccessPolicies)))
		writeJSON(w, http.StatusOK, listed)
		return
	}

	var req access.CreatePolicyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok, err := s.reservations.GetReservation(r.Context(), req.ReservedSubdomainID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "reserved_subdomain_id was not found", http.StatusBadRequest)
		return
	}
	created, err := s.accessPolicies.CreatePolicy(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, access.ErrPolicyAlreadyExists):
			status = http.StatusConflict
		case isAccessPolicyRequestError(err):
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.metrics.accessPolicyAdminCreatesTotal.Add(1)
	s.logAudit(r, slog.LevelInfo, "control-plane access policy created", "control_plane.access_policy_created",
		slog.String("access_policy_id", created.ID),
		slog.String("reserved_subdomain_id", created.ReservedSubdomainID),
		slog.String("mode", string(created.Mode)),
	)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleAccessPolicyByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		methodNotAllowed(w, "GET, PUT, DELETE")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/access-policies/")
	if id == "" || id == "evaluate" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		policy, ok, err := s.accessPolicies.GetPolicy(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, access.ErrPolicyNotFound.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	case http.MethodPut:
		var req access.UpdatePolicyRequest
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		updated, err := s.accessPolicies.UpdatePolicy(r.Context(), id, req)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, access.ErrPolicyNotFound):
				status = http.StatusNotFound
			case isAccessPolicyRequestError(err):
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}
		s.metrics.accessPolicyAdminUpdatesTotal.Add(1)
		s.logAudit(r, slog.LevelInfo, "control-plane access policy updated", "control_plane.access_policy_updated",
			slog.String("access_policy_id", updated.ID),
			slog.String("reserved_subdomain_id", updated.ReservedSubdomainID),
			slog.String("mode", string(updated.Mode)),
		)
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if err := s.accessPolicies.DeletePolicy(r.Context(), id); err != nil {
			if errors.Is(err, access.ErrPolicyNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.metrics.accessPolicyAdminDeletesTotal.Add(1)
		s.logAudit(r, slog.LevelInfo, "control-plane access policy deleted", "control_plane.access_policy_deleted", slog.String("access_policy_id", id))
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleEvaluateAccessPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.validatorAuthorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "validator"),
			slog.Bool("token_configured", s.cfg.ValidatorToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req evaluateAccessPolicyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Subdomain = strings.TrimSpace(req.Subdomain)
	if req.Subdomain == "" {
		http.Error(w, "subdomain is required", http.StatusBadRequest)
		return
	}
	if req.RemoteIP == "" {
		req.RemoteIP = remoteIP(r.RemoteAddr)
	}

	s.metrics.accessPolicyEvaluationsTotal.Add(1)
	result, reservation, err := s.evaluateAccessPolicy(r.Context(), req)
	if err != nil {
		s.metrics.accessPolicyEvaluationErrorsTotal.Add(1)
		status := http.StatusInternalServerError
		if isReservationRequestError(err) {
			status = http.StatusBadRequest
		}
		s.logAudit(r, slog.LevelWarn, "control-plane access policy evaluation failed", "control_plane.access_policy_evaluation_failed",
			slog.String("subdomain", req.Subdomain),
			slog.Int("status", status),
			slog.Any("error", err),
		)
		http.Error(w, err.Error(), status)
		return
	}
	if result.Allowed {
		s.metrics.accessPolicyEvaluationAllowedTotal.Add(1)
	} else {
		s.metrics.accessPolicyEvaluationDeniedTotal.Add(1)
	}
	attrs := []slog.Attr{
		slog.String("subdomain", req.Subdomain),
		slog.String("mode", string(result.Mode)),
		slog.Bool("allowed", result.Allowed),
		slog.String("reason", result.Reason),
	}
	if reservation.ID != "" {
		attrs = append(attrs, slog.String("reservation_id", reservation.ID))
	}
	s.logAudit(r, slog.LevelInfo, "control-plane access policy evaluated", "control_plane.access_policy_evaluated", attrs...)
	writeJSON(w, http.StatusOK, evaluateAccessPolicyResponse{
		Allowed: result.Allowed,
		Mode:    result.Mode,
		Reason:  result.Reason,
	})
}

func (s *Server) handleCustomDomains(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/custom-domains" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, POST")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		listed, err := s.customDomains.ListDomains(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.metrics.customDomainAdminListsTotal.Add(1)
		s.logAudit(r, slog.LevelInfo, "control-plane custom domains listed", "control_plane.custom_domains_listed", slog.Int("count", len(listed.CustomDomains)))
		writeJSON(w, http.StatusOK, listed)
		return
	}

	var req customdomains.CreateDomainRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok, err := s.reservations.GetReservation(r.Context(), req.ReservedSubdomainID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "reserved_subdomain_id was not found", http.StatusBadRequest)
		return
	}
	created, err := s.customDomains.CreateDomain(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, customdomains.ErrDomainAlreadyExists):
			status = http.StatusConflict
		case isCustomDomainRequestError(err):
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.metrics.customDomainAdminCreatesTotal.Add(1)
	s.logAudit(r, slog.LevelInfo, "control-plane custom domain created", "control_plane.custom_domain_created",
		slog.String("custom_domain_id", created.ID),
		slog.String("hostname", created.Hostname),
		slog.String("reserved_subdomain_id", created.ReservedSubdomainID),
		slog.String("status", string(created.Status)),
	)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleCustomDomainByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodDelete && r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, DELETE, POST")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/custom-domains/")
	if strings.HasSuffix(id, "/verify") {
		id = strings.TrimSuffix(id, "/verify")
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		if id == "" || id == "lookup" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		verified, err := s.customDomains.VerifyDomain(r.Context(), id)
		if err != nil {
			if errors.Is(err, customdomains.ErrDomainNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.logAudit(r, slog.LevelInfo, "control-plane custom domain verification checked", "control_plane.custom_domain_verification_checked",
			slog.String("custom_domain_id", verified.ID),
			slog.String("hostname", verified.Hostname),
			slog.String("status", string(verified.Status)),
		)
		writeJSON(w, http.StatusOK, verified)
		return
	}

	if r.Method == http.MethodPost {
		methodNotAllowed(w, "GET, DELETE")
		return
	}
	if id == "" || id == "lookup" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		domain, ok, err := s.customDomains.GetDomain(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, customdomains.ErrDomainNotFound.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, domain)
	case http.MethodDelete:
		if err := s.customDomains.DeleteDomain(r.Context(), id); err != nil {
			if errors.Is(err, customdomains.ErrDomainNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.metrics.customDomainAdminDeletesTotal.Add(1)
		s.logAudit(r, slog.LevelInfo, "control-plane custom domain deleted", "control_plane.custom_domain_deleted", slog.String("custom_domain_id", id))
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleLookupCustomDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.validatorAuthorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "validator"),
			slog.Bool("token_configured", s.cfg.ValidatorToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req lookupCustomDomainRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.metrics.customDomainLookupsTotal.Add(1)
	result, err := s.lookupCustomDomain(r.Context(), req.Hostname)
	if err != nil {
		s.metrics.customDomainLookupErrorsTotal.Add(1)
		status := http.StatusInternalServerError
		if isCustomDomainRequestError(err) {
			status = http.StatusBadRequest
		}
		s.logAudit(r, slog.LevelWarn, "control-plane custom domain lookup failed", "control_plane.custom_domain_lookup_failed",
			slog.String("hostname", req.Hostname),
			slog.Int("status", status),
			slog.Any("error", err),
		)
		http.Error(w, err.Error(), status)
		return
	}
	if result.Found {
		s.metrics.customDomainLookupHitsTotal.Add(1)
	} else {
		s.metrics.customDomainLookupMissesTotal.Add(1)
	}
	s.logAudit(r, slog.LevelInfo, "control-plane custom domain looked up", "control_plane.custom_domain_lookup",
		slog.String("hostname", req.Hostname),
		slog.Bool("found", result.Found),
		slog.String("reason", result.Reason),
		slog.String("custom_domain_id", result.CustomDomainID),
		slog.String("subdomain", result.Subdomain),
	)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) lookupCustomDomain(ctx context.Context, hostname string) (lookupCustomDomainResponse, error) {
	domain, ok, err := s.customDomains.GetDomainByHostname(ctx, hostname)
	if err != nil {
		return lookupCustomDomainResponse{}, err
	}
	if !ok {
		normalized, normalizeErr := customdomains.NormalizeHostname(hostname)
		if normalizeErr != nil {
			return lookupCustomDomainResponse{}, normalizeErr
		}
		return lookupCustomDomainResponse{Found: false, Hostname: normalized, Reason: "not_found"}, nil
	}
	if domain.Status != customdomains.StatusActive {
		reason := "not_verified"
		if domain.Status == customdomains.StatusVerificationFailed {
			reason = "verification_failed"
		}
		return lookupCustomDomainResponse{
			Found:               false,
			Reason:              reason,
			Hostname:            domain.Hostname,
			CustomDomainID:      domain.ID,
			ReservedSubdomainID: domain.ReservedSubdomainID,
			Status:              domain.Status,
		}, nil
	}
	reservation, ok, err := s.reservations.GetReservation(ctx, domain.ReservedSubdomainID)
	if err != nil {
		return lookupCustomDomainResponse{}, err
	}
	if !ok {
		return lookupCustomDomainResponse{
			Found:               false,
			Reason:              "reservation_not_found",
			Hostname:            domain.Hostname,
			CustomDomainID:      domain.ID,
			ReservedSubdomainID: domain.ReservedSubdomainID,
			Status:              domain.Status,
		}, nil
	}
	return lookupCustomDomainResponse{
		Found:               true,
		Hostname:            domain.Hostname,
		CustomDomainID:      domain.ID,
		ReservedSubdomainID: domain.ReservedSubdomainID,
		Subdomain:           reservation.Name,
		Status:              domain.Status,
	}, nil
}

func (s *Server) evaluateAccessPolicy(ctx context.Context, req evaluateAccessPolicyRequest) (access.CheckPolicyResult, reserved.ReservationSummary, error) {
	reservation, ok, err := s.reservations.GetReservationByName(ctx, req.Subdomain)
	if err != nil {
		return access.CheckPolicyResult{}, reserved.ReservationSummary{}, err
	}
	if !ok {
		return access.CheckPolicyResult{Allowed: true, Mode: access.ModePublic, Reason: "not_reserved"}, reserved.ReservationSummary{}, nil
	}

	result, err := s.accessPolicies.CheckPolicy(ctx, access.CheckPolicyRequest{
		ReservedSubdomainID: reservation.ID,
		RemoteIP:            req.RemoteIP,
		BasicUsername:       req.BasicUsername,
		BasicPassword:       req.BasicPassword,
		BearerToken:         req.BearerToken,
	})
	if err != nil {
		return access.CheckPolicyResult{}, reserved.ReservationSummary{}, err
	}
	return result, reservation, nil
}

func (s *Server) handleReservedSubdomainByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, "DELETE")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/reserved-subdomains/")
	if id == "" || id == "authorize" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if err := s.reservations.DeleteReservation(r.Context(), id); err != nil {
		if errors.Is(err, reserved.ErrReservationNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.metrics.reservationAdminDeletesTotal.Add(1)
	s.logAudit(r, slog.LevelInfo, "control-plane reserved subdomain deleted", "control_plane.reservation_deleted", slog.String("reservation_id", id))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTokenByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, "DELETE")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
		s.logAudit(r, slog.LevelWarn, "control-plane authorization failed", "control_plane.auth_failed",
			slog.String("surface", "admin"),
			slog.Bool("token_configured", s.cfg.AdminToken != ""),
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/tokens/")
	if id == "" || id == "validate" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if err := s.service.RevokeToken(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.metrics.tokenAdminRevokesTotal.Add(1)
	s.logAudit(r, slog.LevelInfo, "control-plane token revoked", "control_plane.token_revoked", slog.String("token_id", id))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorized(r *http.Request) bool {
	return authorizedBearer(r, s.cfg.AdminToken)
}

func (s *Server) validatorAuthorized(r *http.Request) bool {
	return authorizedBearer(r, s.cfg.ValidatorToken)
}

func authorizedBearer(r *http.Request, configuredToken string) bool {
	if configuredToken == "" {
		return false
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && secretEqual(token, configuredToken)
}

func (s *Server) ready(ctx context.Context) error {
	if err := s.service.Ready(ctx); err != nil {
		return err
	}
	if err := s.reservations.Ready(ctx); err != nil {
		return err
	}
	if err := s.accessPolicies.Ready(ctx); err != nil {
		return err
	}
	return s.customDomains.Ready(ctx)
}

func secretEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}

func (s *Server) logAudit(r *http.Request, level slog.Level, msg, event string, attrs ...slog.Attr) {
	base := []slog.Attr{
		slog.String("event", event),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("remote_ip", remoteIP(r.RemoteAddr)),
	}
	if id := requestID(r); id != "" {
		base = append(base, slog.String("request_id", id))
	}
	base = append(base, attrs...)
	s.logger.LogAttrs(r.Context(), level, msg, base...)
	s.auditEvents.add(auditEvent{
		Time:      time.Now().UTC(),
		Level:     level.String(),
		Message:   msg,
		Event:     event,
		Method:    r.Method,
		Path:      r.URL.Path,
		RemoteIP:  remoteIP(r.RemoteAddr),
		RequestID: requestID(r),
		Fields:    auditEventFields(attrs),
	})
}

func auditEventFields(attrs []slog.Attr) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	fields := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}
		value := attr.Value.Resolve()
		fields[attr.Key] = auditEventFieldValue(value)
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func auditEventFieldValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	case slog.KindBool:
		if value.Bool() {
			return "true"
		}
		return "false"
	case slog.KindInt64:
		return fmt.Sprintf("%d", value.Int64())
	case slog.KindUint64:
		return fmt.Sprintf("%d", value.Uint64())
	case slog.KindFloat64:
		return fmt.Sprintf("%g", value.Float64())
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(value.Any())
	}
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func requestID(r *http.Request) string {
	return requestid.FromRequest(r)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return fmt.Errorf("json body is too large: maximum %d bytes", maxJSONBodyBytes)
		}
		if errors.Is(err, io.EOF) {
			return errors.New("json body is required")
		}
		return fmt.Errorf("invalid json: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("invalid json: %w", err)
	}
	return errors.New("invalid json: multiple json values")
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func isTokenRequestError(err error) bool {
	return errors.Is(err, tokens.ErrTokenNameRequired) || errors.Is(err, tokens.ErrUnsupportedScope)
}

func isReservationRequestError(err error) bool {
	return errors.Is(err, reserved.ErrReservationNameRequired) ||
		errors.Is(err, reserved.ErrReservationTokenIDRequired) ||
		strings.Contains(err.Error(), "invalid reserved subdomain")
}

func isAccessPolicyRequestError(err error) bool {
	return errors.Is(err, access.ErrPolicyReservedSubdomainIDRequired) ||
		errors.Is(err, access.ErrPolicyModeRequired) ||
		errors.Is(err, access.ErrPolicyModeUnsupported) ||
		errors.Is(err, access.ErrPolicySecretRequired) ||
		errors.Is(err, access.ErrPolicyBasicUsernameRequired) ||
		errors.Is(err, access.ErrPolicyIPAllowlistRequired) ||
		strings.Contains(err.Error(), "invalid ip allowlist")
}

func isCustomDomainRequestError(err error) bool {
	return errors.Is(err, customdomains.ErrDomainHostnameRequired) ||
		errors.Is(err, customdomains.ErrDomainReservedSubdomainIDRequired) ||
		errors.Is(err, customdomains.ErrDomainInvalidHostname)
}
