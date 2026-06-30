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
)

type Server struct {
	cfg     Config
	service *tokens.Service
	logger  *slog.Logger
}

type validateTokenRequest struct {
	Token string `json:"token"`
	Scope string `json:"scope,omitempty"`
}

const maxJSONBodyBytes = 1 << 20

func NewServer(cfg Config, service *tokens.Service) *Server {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
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
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.HandleFunc("/api/v1/tokens", s.handleTokens)
	mux.HandleFunc("/api/v1/tokens/", s.handleTokenByID)
	mux.HandleFunc("/api/v1/tokens/validate", s.handleValidateToken)
	return mux
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet {
		listed, err := s.service.ListTokens(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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
	s.logger.Info("control-plane token created", "token_id", created.ID, "name", created.Name, "scopes", created.Scopes)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleValidateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.validatorAuthorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req validateTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.service.ValidateToken(r.Context(), req.Token, req.Scope)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, tokens.ErrUnsupportedScope) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTokenByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, "DELETE")
		return
	}
	if !s.authorized(r) {
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
