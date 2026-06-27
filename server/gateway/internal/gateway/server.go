// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/names"
	"github.com/voiteco/porthook/server/gateway/internal/registry"
)

const agentWebSocketPath = "/agent/connect"

const (
	randomSubdomainLength   = 8
	randomSubdomainAttempts = 8
)

type Server struct {
	cfg      Config
	logger   *slog.Logger
	registry *registry.Registry

	sessionsMu sync.RWMutex
	sessions   map[string]*agentSession

	generateSubdomain func(length int) (string, error)
	generateTunnelID  func() (string, error)
}

func NewServer(cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	cfg = normalizeConfig(cfg)

	return &Server{
		cfg:      cfg,
		logger:   logger,
		registry: registry.New(),
		sessions: make(map[string]*agentSession),

		generateSubdomain: names.RandomSubdomain,
		generateTunnelID:  newTunnelID,
	}
}

func (s *Server) Run(ctx context.Context) error {
	publicServer := &http.Server{
		Addr:              s.cfg.PublicAddr,
		Handler:           s.PublicHandler(),
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}
	agentServer := &http.Server{
		Addr:              s.cfg.AgentAddr,
		Handler:           s.AgentHandler(),
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}

	errCh := make(chan error, 2)
	go serveHTTP(errCh, publicServer)
	go serveHTTP(errCh, agentServer)

	s.logger.Info("gateway started", "public_addr", s.cfg.PublicAddr, "agent_addr", s.cfg.AgentAddr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		s.closeSessions(shutdownCtx, websocket.StatusGoingAway, "gateway shutting down")
		errPublic := publicServer.Shutdown(shutdownCtx)
		errAgent := agentServer.Shutdown(shutdownCtx)
		return errors.Join(errPublic, errAgent)
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		s.closeSessions(shutdownCtx, websocket.StatusInternalError, "gateway listener stopped")
		errPublic := publicServer.Shutdown(shutdownCtx)
		errAgent := agentServer.Shutdown(shutdownCtx)
		return errors.Join(err, errPublic, errAgent)
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
	mux.HandleFunc("/", s.handlePublicRequest)
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

	ctx, cancel := contextWithTimeout(r.Context(), s.cfg.HandshakeTimeout)
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

	stopKeepalive := session.startKeepalive(r.Context(), s.cfg.WebSocketPingInterval, s.cfg.WebSocketPongTimeout, s.logger)
	defer stopKeepalive()

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
		_ = writeTunnelError(ctx, conn, "unsupported_protocol", fmt.Sprintf("unsupported tunnel protocol %q", payload.Protocol))
		return nil, fmt.Errorf("unsupported tunnel protocol %q", payload.Protocol)
	}

	tunnel, err := s.registerTunnelSession(ctx, conn, payload)
	if err != nil {
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

	return newAgentSession(conn, tunnel, s.cfg.WebSocketWriteTimeout), nil
}

func (s *Server) registerTunnelSession(ctx context.Context, conn *websocket.Conn, payload messages.TunnelRegister) (*registry.Session, error) {
	if payload.RequestedSubdomain != "" {
		return s.registerRequestedSubdomain(ctx, conn, payload)
	}
	return s.registerRandomSubdomain(ctx, conn, payload)
}

func (s *Server) registerRequestedSubdomain(ctx context.Context, conn *websocket.Conn, payload messages.TunnelRegister) (*registry.Session, error) {
	subdomain := payload.RequestedSubdomain
	if err := names.ValidateSubdomain(subdomain); err != nil {
		message := fmt.Sprintf("invalid requested subdomain %q: %s", subdomain, err)
		_ = writeTunnelError(ctx, conn, "invalid_subdomain", message)
		return nil, errors.New(message)
	}

	tunnel, err := s.newTunnelSession(payload, subdomain)
	if err != nil {
		_ = writeTunnelError(ctx, conn, "registration_failed", "failed to create tunnel session")
		return nil, err
	}

	if err := s.registry.Register(tunnel); err != nil {
		if errors.Is(err, registry.ErrSubdomainAlreadyRegistered) {
			message := fmt.Sprintf("subdomain %q is already in use; choose another name or omit --subdomain for a random subdomain", subdomain)
			_ = writeTunnelError(ctx, conn, "subdomain_unavailable", message)
			return nil, err
		}
		_ = writeTunnelError(ctx, conn, "registration_failed", err.Error())
		return nil, err
	}

	return tunnel, nil
}

func (s *Server) registerRandomSubdomain(ctx context.Context, conn *websocket.Conn, payload messages.TunnelRegister) (*registry.Session, error) {
	var lastErr error
	for attempt := 0; attempt < randomSubdomainAttempts; attempt++ {
		subdomain, err := s.generateSubdomain(randomSubdomainLength)
		if err != nil {
			_ = writeTunnelError(ctx, conn, "registration_failed", "failed to generate random subdomain")
			return nil, err
		}
		if err := names.ValidateSubdomain(subdomain); err != nil {
			lastErr = err
			continue
		}

		tunnel, err := s.newTunnelSession(payload, subdomain)
		if err != nil {
			_ = writeTunnelError(ctx, conn, "registration_failed", "failed to create tunnel session")
			return nil, err
		}

		if err := s.registry.Register(tunnel); err != nil {
			if errors.Is(err, registry.ErrSubdomainAlreadyRegistered) {
				lastErr = err
				continue
			}
			_ = writeTunnelError(ctx, conn, "registration_failed", err.Error())
			return nil, err
		}

		return tunnel, nil
	}

	message := "could not assign a random subdomain; retry or request a subdomain with --subdomain"
	_ = writeTunnelError(ctx, conn, "random_subdomain_exhausted", message)
	if lastErr != nil {
		return nil, fmt.Errorf("%s: %w", message, lastErr)
	}
	return nil, errors.New(message)
}

func (s *Server) newTunnelSession(payload messages.TunnelRegister, subdomain string) (*registry.Session, error) {
	tunnelID, err := s.generateTunnelID()
	if err != nil {
		return nil, err
	}

	return &registry.Session{
		TunnelID:    tunnelID,
		Subdomain:   subdomain,
		PublicURL:   s.publicURLForSubdomain(subdomain),
		LocalTarget: payload.LocalTarget,
		Protocol:    payload.Protocol,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func writeTunnelError(ctx context.Context, conn *websocket.Conn, code, message string) error {
	tunnelErr, err := messages.New(messages.TypeTunnelError, messages.ErrorPayload{
		Code:    code,
		Message: message,
	})
	if err != nil {
		return err
	}
	return wsjson.Write(ctx, conn, tunnelErr)
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

func (s *Server) handlePublicRequest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	status := 0
	outcome := "unknown"
	var subdomain string
	var tunnelID string
	var streamID string
	var requestBytes int64
	var responseBytes int64
	var requestErr error
	defer func() {
		if status == 0 {
			status = http.StatusInternalServerError
		}
		s.logPublicRequest(r, publicRequestLog{
			Started:       started,
			Status:        status,
			Outcome:       outcome,
			Subdomain:     subdomain,
			TunnelID:      tunnelID,
			StreamID:      streamID,
			RequestBytes:  requestBytes,
			ResponseBytes: responseBytes,
			Error:         requestErr,
		})
	}()

	subdomain, ok := s.subdomainFromHost(r.Host)
	if !ok {
		status = http.StatusNotFound
		outcome = "no_tunnel_for_host"
		http.Error(w, fmt.Sprintf("no active tunnel for host %q", r.Host), http.StatusNotFound)
		return
	}

	session, ok := s.sessionBySubdomain(subdomain)
	if !ok {
		status = http.StatusNotFound
		outcome = "no_active_session"
		http.Error(w, fmt.Sprintf("no active tunnel for host %q", r.Host), http.StatusNotFound)
		return
	}
	tunnelID = session.tunnel.TunnelID

	body, err := readLimitedBody(w, r, s.cfg.MaxBodyBytes)
	if err != nil {
		status = http.StatusRequestEntityTooLarge
		outcome = "request_body_too_large"
		requestErr = err
		requestBytes = requestContentLength(r, s.cfg.MaxBodyBytes+1)
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	requestBytes = int64(len(body))

	streamID, err = newStreamID()
	if err != nil {
		status = http.StatusInternalServerError
		outcome = "stream_id_failed"
		requestErr = err
		http.Error(w, "failed to create stream", http.StatusInternalServerError)
		return
	}

	requestHeader := httpwire.StripHopByHopHeaders(r.Header)
	requestHeader = httpwire.AddForwardedHeaders(
		requestHeader,
		session.tunnel.TunnelID,
		r.Host,
		requestProto(r),
		remoteIP(r.RemoteAddr),
	)

	tunnelRequest := httpwire.Request{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Header: requestHeader,
		Body:   body,
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.StreamTimeout)
	defer cancel()

	tunnelResponse, err := session.roundTrip(ctx, streamID, tunnelRequest)
	if err != nil {
		requestErr = err
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			outcome = "tunnel_timeout"
			http.Error(w, "tunnel request timed out", http.StatusGatewayTimeout)
			return
		}
		status = http.StatusBadGateway
		outcome = "tunnel_error"
		http.Error(w, "tunnel request failed", http.StatusBadGateway)
		return
	}

	for name, values := range httpwire.StripHopByHopHeaders(tunnelResponse.Header) {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	if tunnelResponse.Status <= 0 {
		tunnelResponse.Status = http.StatusBadGateway
	}
	status = tunnelResponse.Status
	outcome = "completed"
	responseBytes = int64(len(tunnelResponse.Body))
	w.WriteHeader(tunnelResponse.Status)
	_, _ = w.Write(tunnelResponse.Body)
}

type publicRequestLog struct {
	Started       time.Time
	Status        int
	Outcome       string
	Subdomain     string
	TunnelID      string
	StreamID      string
	RequestBytes  int64
	ResponseBytes int64
	Error         error
}

func (s *Server) logPublicRequest(r *http.Request, entry publicRequestLog) {
	level := slog.LevelInfo
	if entry.Status >= 500 {
		level = slog.LevelWarn
	}

	attrs := []slog.Attr{
		slog.String("method", r.Method),
		slog.String("host", r.Host),
		slog.String("path", r.URL.Path),
		slog.String("query", r.URL.RawQuery),
		slog.String("remote_ip", remoteIP(r.RemoteAddr)),
		slog.String("subdomain", entry.Subdomain),
		slog.String("tunnel_id", entry.TunnelID),
		slog.String("stream_id", entry.StreamID),
		slog.Int("status", entry.Status),
		slog.String("outcome", entry.Outcome),
		slog.Int64("request_bytes", entry.RequestBytes),
		slog.Int64("response_bytes", entry.ResponseBytes),
		slog.Int64("duration_ms", time.Since(entry.Started).Milliseconds()),
	}
	if entry.Error != nil {
		attrs = append(attrs, slog.Any("error", entry.Error))
	}

	s.logger.LogAttrs(r.Context(), level, "public request", attrs...)
}

func (s *Server) subdomainFromHost(hostport string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(hostport))
	if host == "" {
		return "", false
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}

	suffix := "." + strings.ToLower(strings.TrimSpace(s.cfg.RootDomain))
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}

	subdomain := strings.TrimSuffix(host, suffix)
	if subdomain == "" || strings.Contains(subdomain, ".") {
		return "", false
	}
	if err := names.ValidateSubdomain(subdomain); err != nil {
		return "", false
	}
	return subdomain, true
}

func (s *Server) sessionBySubdomain(subdomain string) (*agentSession, bool) {
	tunnel, ok := s.registry.LookupBySubdomain(subdomain)
	if !ok {
		return nil, false
	}

	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	session, ok := s.sessions[tunnel.TunnelID]
	return session, ok
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

func (s *Server) closeSessions(ctx context.Context, status websocket.StatusCode, reason string) {
	s.sessionsMu.RLock()
	sessions := make([]*agentSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessionsMu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(sessions))
	for _, session := range sessions {
		go func(session *agentSession) {
			defer wg.Done()
			if err := session.close(status, reason); err != nil {
				s.logger.Warn("close tunnel session failed", "tunnel_id", session.tunnel.TunnelID, "error", err)
			}
		}(session)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		for _, session := range sessions {
			if err := session.closeNow(); err != nil {
				s.logger.Warn("force close tunnel session failed", "tunnel_id", session.tunnel.TunnelID, "error", err)
			}
		}
	}
}

func newTunnelID() (string, error) {
	suffix, err := names.RandomSubdomain(20)
	if err != nil {
		return "", err
	}
	return "tun_" + suffix, nil
}

func newStreamID() (string, error) {
	suffix, err := names.RandomSubdomain(20)
	if err != nil {
		return "", err
	}
	return "str_" + suffix, nil
}

func readLimitedBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = defaultMaxBodyBytes
	}
	defer r.Body.Close()
	return io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
}

func requestContentLength(r *http.Request, fallback int64) int64 {
	if r.ContentLength >= 0 {
		return r.ContentLength
	}
	return fallback
}

func serveHTTP(errCh chan<- error, server *http.Server) {
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
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
