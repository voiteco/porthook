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

	"github.com/voiteco/porthook/server/control-plane/internal/reserved"
	"github.com/voiteco/porthook/server/control-plane/internal/tokens"
	"github.com/voiteco/porthook/server/dashboard"
)

type Server struct {
	cfg          Config
	service      *tokens.Service
	reservations *reserved.Service
	logger       *slog.Logger
	metrics      metrics
}

type validateTokenRequest struct {
	Token string `json:"token"`
	Scope string `json:"scope,omitempty"`
}

type authorizeReservedSubdomainRequest struct {
	TokenID   string `json:"token_id"`
	Subdomain string `json:"subdomain"`
}

type statusResponse struct {
	Status  string `json:"status"`
	Ready   bool   `json:"ready"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

const maxJSONBodyBytes = 1 << 20

const readinessTimeout = 2 * time.Second

func NewServer(cfg Config, service *tokens.Service, reservationServices ...*reserved.Service) *Server {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if service == nil {
		service = tokens.NewService(tokens.NewMemoryStore())
	}
	reservationService := reserved.NewService(reserved.NewMemoryStore())
	if len(reservationServices) > 0 && reservationServices[0] != nil {
		reservationService = reservationServices[0]
	}
	return &Server{
		cfg:          cfg,
		service:      service,
		reservations: reservationService,
		logger:       slog.Default(),
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
	mux.HandleFunc("/api/v1/tokens", s.handleTokens)
	mux.HandleFunc("/api/v1/tokens/", s.handleTokenByID)
	mux.HandleFunc("/api/v1/tokens/validate", s.handleValidateToken)
	mux.HandleFunc("/api/v1/reserved-subdomains", s.handleReservedSubdomains)
	mux.HandleFunc("/api/v1/reserved-subdomains/", s.handleReservedSubdomainByID)
	mux.HandleFunc("/api/v1/reserved-subdomains/authorize", s.handleAuthorizeReservedSubdomain)
	return mux
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
	return s.reservations.Ready(ctx)
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
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func requestID(r *http.Request) string {
	for _, header := range []string{"X-Request-ID", "X-Correlation-ID"} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	return ""
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
