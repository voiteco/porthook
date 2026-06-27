// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/names"
	"github.com/voiteco/porthook/server/gateway/internal/registry"
)

const agentWebSocketPath = "/agent/connect"

type Server struct {
	cfg      Config
	logger   *slog.Logger
	registry *registry.Registry

	sessionsMu sync.RWMutex
	sessions   map[string]*agentSession
}

func NewServer(cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		cfg:      cfg,
		logger:   logger,
		registry: registry.New(),
		sessions: make(map[string]*agentSession),
	}
}

func (s *Server) Run(ctx context.Context) error {
	publicServer := &http.Server{
		Addr:              s.cfg.PublicAddr,
		Handler:           s.PublicHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	agentServer := &http.Server{
		Addr:              s.cfg.AgentAddr,
		Handler:           s.AgentHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)
	go serveHTTP(errCh, publicServer)
	go serveHTTP(errCh, agentServer)

	s.logger.Info("gateway started", "public_addr", s.cfg.PublicAddr, "agent_addr", s.cfg.AgentAddr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		errPublic := publicServer.Shutdown(shutdownCtx)
		errAgent := agentServer.Shutdown(shutdownCtx)
		return errors.Join(errPublic, errAgent)
	case err := <-errCh:
		return err
	}
}

func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, fmt.Sprintf("no active tunnel for host %q", r.Host), http.StatusNotFound)
	})
	return mux
}

func (s *Server) AgentHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(agentWebSocketPath, s.handleAgentWebSocket)
	return mux
}

func (s *Server) handleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		s.logger.Warn("agent websocket accept failed", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.StreamTimeout)
	defer cancel()

	if err := s.authenticateAgent(ctx, conn); err != nil {
		s.logger.Warn("agent authentication failed", "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "authentication failed")
		return
	}

	session, err := s.registerTunnel(ctx, conn)
	if err != nil {
		s.logger.Warn("tunnel registration failed", "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "registration failed")
		return
	}

	s.addSession(session)
	defer s.removeSession(session.tunnel.TunnelID)

	s.logger.Info("tunnel registered",
		"tunnel_id", session.tunnel.TunnelID,
		"subdomain", session.tunnel.Subdomain,
		"local_target", session.tunnel.LocalTarget,
	)

	session.readLoop(r.Context(), s.logger)
}

func (s *Server) authenticateAgent(ctx context.Context, conn *websocket.Conn) error {
	var env messages.Envelope
	if err := wsjson.Read(ctx, conn, &env); err != nil {
		return fmt.Errorf("read auth request: %w", err)
	}
	if env.Type != messages.TypeAuthRequest {
		return fmt.Errorf("expected %s, got %s", messages.TypeAuthRequest, env.Type)
	}

	payload, err := messages.DecodePayload[messages.AuthRequest](env)
	if err != nil {
		return err
	}
	if payload.Token == "" || payload.Token != s.cfg.StaticToken {
		authErr, _ := messages.New(messages.TypeAuthError, messages.ErrorPayload{
			Code:    "invalid_token",
			Message: "invalid token",
		})
		_ = wsjson.Write(ctx, conn, authErr)
		return errors.New("invalid token")
	}

	authOK, err := messages.New(messages.TypeAuthOK, messages.AuthOK{})
	if err != nil {
		return err
	}
	return wsjson.Write(ctx, conn, authOK)
}

func (s *Server) registerTunnel(ctx context.Context, conn *websocket.Conn) (*agentSession, error) {
	var env messages.Envelope
	if err := wsjson.Read(ctx, conn, &env); err != nil {
		return nil, fmt.Errorf("read tunnel registration: %w", err)
	}
	if env.Type != messages.TypeTunnelRegister {
		return nil, fmt.Errorf("expected %s, got %s", messages.TypeTunnelRegister, env.Type)
	}

	payload, err := messages.DecodePayload[messages.TunnelRegister](env)
	if err != nil {
		return nil, err
	}
	if payload.Protocol != "http" {
		return nil, fmt.Errorf("unsupported tunnel protocol %q", payload.Protocol)
	}

	subdomain := payload.RequestedSubdomain
	if subdomain == "" {
		subdomain, err = names.RandomSubdomain(8)
		if err != nil {
			return nil, err
		}
	}
	if err := names.ValidateSubdomain(subdomain); err != nil {
		return nil, err
	}

	tunnelID, err := newTunnelID()
	if err != nil {
		return nil, err
	}

	tunnel := &registry.Session{
		TunnelID:    tunnelID,
		Subdomain:   subdomain,
		PublicURL:   s.publicURLForSubdomain(subdomain),
		LocalTarget: payload.LocalTarget,
		Protocol:    payload.Protocol,
		CreatedAt:   time.Now().UTC(),
	}

	if err := s.registry.Register(tunnel); err != nil {
		tunnelErr, _ := messages.New(messages.TypeTunnelError, messages.ErrorPayload{
			Code:    "registration_failed",
			Message: err.Error(),
		})
		_ = wsjson.Write(ctx, conn, tunnelErr)
		return nil, err
	}

	registered, err := messages.New(messages.TypeTunnelRegistered, messages.TunnelRegistered{
		TunnelID:  tunnel.TunnelID,
		PublicURL: tunnel.PublicURL,
		Subdomain: tunnel.Subdomain,
	})
	if err != nil {
		s.registry.Unregister(tunnel.TunnelID)
		return nil, err
	}
	if err := wsjson.Write(ctx, conn, registered); err != nil {
		s.registry.Unregister(tunnel.TunnelID)
		return nil, fmt.Errorf("write tunnel registered: %w", err)
	}

	return newAgentSession(conn, tunnel), nil
}

func (s *Server) publicURLForSubdomain(subdomain string) string {
	parsed, err := url.Parse(s.cfg.PublicURL)
	if err != nil || parsed.Scheme == "" {
		return "http://" + subdomain + "." + s.cfg.RootDomain
	}

	host := s.cfg.RootDomain
	if _, port, err := net.SplitHostPort(parsed.Host); err == nil && port != "" {
		host = net.JoinHostPort(subdomain+"."+s.cfg.RootDomain, port)
	} else {
		host = subdomain + "." + s.cfg.RootDomain
	}

	parsed.Host = host
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (s *Server) addSession(session *agentSession) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[session.tunnel.TunnelID] = session
}

func (s *Server) removeSession(tunnelID string) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	delete(s.sessions, tunnelID)
	s.registry.Unregister(tunnelID)
	s.logger.Info("tunnel disconnected", "tunnel_id", tunnelID)
}

func newTunnelID() (string, error) {
	suffix, err := names.RandomSubdomain(20)
	if err != nil {
		return "", err
	}
	return "tun_" + suffix, nil
}

func serveHTTP(errCh chan<- error, server *http.Server) {
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}

func requestProto(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return proto
	}
	return "http"
}
