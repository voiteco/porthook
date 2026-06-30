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
	"net/http"
	"strings"
	"time"

	"github.com/voiteco/porthook/server/control-plane/internal/tokens"
	"github.com/voiteco/porthook/server/dashboard"
)

type Server struct {
	cfg     Config
	service *tokens.Service
	logger  *slog.Logger
	metrics metrics
}

type validateTokenRequest struct {
	Token string `json:"token"`
	Scope string `json:"scope,omitempty"`
}

type statusResponse struct {
	Status  string `json:"status"`
	Ready   bool   `json:"ready"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

const maxJSONBodyBytes = 1 << 20

const readinessTimeout = 2 * time.Second

func NewServer(cfg Config, service *tokens.Service) *Server {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	return &Server{
		cfg:     cfg,
		service: service,
		logger:  slog.Default(),
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
	return mux
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := s.service.Ready(ctx); err != nil {
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
	if err := s.service.Ready(ctx); err != nil {
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
		s.logger.Info("control-plane tokens listed", "count", len(listed.Tokens))
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
	s.logger.Info("control-plane token created", "token_id", created.ID, "name", created.Name, "scopes", created.Scopes)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleValidateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.validatorAuthorized(r) {
		s.metrics.authFailuresTotal.Add(1)
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
		http.Error(w, err.Error(), status)
		return
	}
	if result.Valid {
		s.metrics.tokenValidationValidTotal.Add(1)
	} else {
		s.metrics.tokenValidationInvalidTotal.Add(1)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTokenByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, "DELETE")
		return
	}
	if !s.authorized(r) {
		s.metrics.authFailuresTotal.Add(1)
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
	s.logger.Info("control-plane token revoked", "token_id", id)
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

func secretEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
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
