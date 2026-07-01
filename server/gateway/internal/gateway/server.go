// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/voiteco/porthook/protocol/httpwire"
	"github.com/voiteco/porthook/protocol/messages"
	"github.com/voiteco/porthook/protocol/names"
	"github.com/voiteco/porthook/protocol/wswire"
	"github.com/voiteco/porthook/server/gateway/internal/registry"
	"github.com/voiteco/porthook/server/internal/requestid"
	"github.com/voiteco/porthook/server/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const agentWebSocketPath = "/agent/connect"

const (
	randomSubdomainLength   = 8
	randomSubdomainAttempts = 8
)

type Server struct {
	cfg         Config
	logger      *slog.Logger
	registry    *registry.Registry
	tokens      agentTokenValidator
	reserved    reservedSubdomainAuthorizer
	access      accessPolicyEvaluator
	domains     customDomainResolver
	metrics     metrics
	startedAt   time.Time
	requestLogs *requestLogBuffer

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
	tokenValidator, err := newAgentTokenValidator(cfg)
	if err != nil {
		logger.Warn("gateway token validator configuration failed", "event", "gateway.token_validator_config_failed", "error", err)
		tokenValidator = errorTokenValidator{err: err}
	}
	reservedAuthorizer, err := newReservedSubdomainAuthorizer(cfg)
	if err != nil {
		logger.Warn("gateway reserved subdomain authorizer configuration failed", "event", "gateway.reserved_authorizer_config_failed", "error", err)
		reservedAuthorizer = errorReservedSubdomainAuthorizer{err: err}
	}
	accessPolicyEvaluator, err := newAccessPolicyEvaluator(cfg)
	if err != nil {
		logger.Warn("gateway access policy evaluator configuration failed", "event", "gateway.access_policy_evaluator_config_failed", "error", err)
		accessPolicyEvaluator = errorAccessPolicyEvaluator{err: err}
	}
	customDomainResolver, err := newCustomDomainResolver(cfg)
	if err != nil {
		logger.Warn("gateway custom domain resolver configuration failed", "event", "gateway.custom_domain_resolver_config_failed", "error", err)
		customDomainResolver = errorCustomDomainResolver{err: err}
	}

	return &Server{
		cfg:         cfg,
		logger:      logger,
		registry:    registry.New(),
		tokens:      tokenValidator,
		reserved:    reservedAuthorizer,
		access:      accessPolicyEvaluator,
		domains:     customDomainResolver,
		startedAt:   time.Now().UTC(),
		requestLogs: newRequestLogBuffer(cfg.RequestLogLimit),
		sessions:    make(map[string]*agentSession),

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

	s.logger.Info("gateway started", "event", "gateway.started", "public_addr", s.cfg.PublicAddr, "agent_addr", s.cfg.AgentAddr)

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
	mux.HandleFunc("/api/v1/tunnels/", s.handleTunnelDetail)
	mux.HandleFunc("/api/v1/tunnels", s.handleTunnelList)
	mux.HandleFunc("/api/v1/request-logs", s.handleRequestLogs)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/", s.handlePublicRequest)
	return requestid.Middleware(telemetry.HTTPHandler(mux, "gateway.public"))
}

func (s *Server) AgentHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(agentWebSocketPath, s.handleAgentWebSocket)
	return requestid.Middleware(telemetry.HTTPHandler(mux, "gateway.agent"))
}

func (s *Server) handleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		attrs := requestAuditAttrs(r, "gateway.agent_websocket_accept_failed")
		attrs = append(attrs, slog.Any("error", err))
		s.logger.LogAttrs(r.Context(), slog.LevelWarn, "agent websocket accept failed", attrs...)
		return
	}
	conn.SetReadLimit(wswire.ReadLimitForChunkBytes(s.cfg.StreamChunkBytes))
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := contextWithTimeout(r.Context(), s.cfg.HandshakeTimeout)
	defer cancel()

	identity, err := s.authenticateAgent(ctx, conn)
	if err != nil {
		s.metrics.authFailuresTotal.Add(1)
		attrs := requestAuditAttrs(r, "gateway.agent_auth_failed")
		attrs = append(attrs, slog.Any("error", err))
		s.logger.LogAttrs(r.Context(), slog.LevelWarn, "agent authentication failed", attrs...)
		_ = conn.Close(websocket.StatusPolicyViolation, "authentication failed")
		return
	}

	session, err := s.registerTunnel(ctx, conn, identity)
	if err != nil {
		s.metrics.tunnelRegistrationFailuresTotal.Add(1)
		attrs := requestAuditAttrs(r, "gateway.tunnel_registration_failed")
		attrs = append(attrs, slog.String("token_id", identity.TokenID), slog.Any("error", err))
		s.logger.LogAttrs(r.Context(), slog.LevelWarn, "tunnel registration failed", attrs...)
		_ = conn.Close(websocket.StatusPolicyViolation, "registration failed")
		return
	}

	s.addSession(session)
	defer s.removeSession(session.tunnel.TunnelID)

	s.logger.Info("tunnel registered",
		"event", "gateway.tunnel_registered",
		"tunnel_id", session.tunnel.TunnelID,
		"subdomain", session.tunnel.Subdomain,
		"token_id", identity.TokenID,
		"protocol", session.tunnel.Protocol,
		"local_target", session.tunnel.LocalTarget,
	)
	s.metrics.tunnelRegistrationsTotal.Add(1)

	stopKeepalive := session.startKeepalive(r.Context(), s.cfg.WebSocketPingInterval, s.cfg.WebSocketPongTimeout, s.logger)
	defer stopKeepalive()

	session.readLoop(r.Context(), s.logger)
}

type tunnelListResponse struct {
	Tunnels []tunnelSummary `json:"tunnels"`
}

type tunnelSummary struct {
	TunnelID    string    `json:"tunnel_id"`
	Subdomain   string    `json:"subdomain"`
	PublicURL   string    `json:"public_url"`
	Protocol    string    `json:"protocol"`
	ConnectedAt time.Time `json:"connected_at"`
}

type tunnelDetailResponse struct {
	Tunnel tunnelDetail `json:"tunnel"`
}

type tunnelDetail struct {
	TunnelID         string               `json:"tunnel_id"`
	Subdomain        string               `json:"subdomain"`
	PublicURL        string               `json:"public_url"`
	Protocol         string               `json:"protocol"`
	AgentVersion     string               `json:"agent_version,omitempty"`
	ProtocolVersion  string               `json:"protocol_version,omitempty"`
	ConnectedAt      time.Time            `json:"connected_at"`
	ConnectedSeconds int64                `json:"connected_seconds"`
	ActiveStreams    int                  `json:"active_streams"`
	StreamCapacity   int                  `json:"stream_capacity"`
	RecentRequests   tunnelRequestSummary `json:"recent_requests"`
}

type tunnelRequestSummary struct {
	Count         int        `json:"count"`
	LastRequestAt *time.Time `json:"last_request_at,omitempty"`
	LastStatus    int        `json:"last_status,omitempty"`
	LastOutcome   string     `json:"last_outcome,omitempty"`
	LastRequestID string     `json:"last_request_id,omitempty"`
	ErrorCount    int        `json:"error_count"`
	CustomDomains []string   `json:"custom_domains,omitempty"`
}

func (s *Server) handleTunnelList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET, OPTIONS")
		return
	}

	sessions := s.registry.List()
	tunnels := make([]tunnelSummary, 0, len(sessions))
	for _, session := range sessions {
		tunnels = append(tunnels, tunnelSummary{
			TunnelID:    session.TunnelID,
			Subdomain:   session.Subdomain,
			PublicURL:   session.PublicURL,
			Protocol:    session.Protocol,
			ConnectedAt: session.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, tunnelListResponse{Tunnels: tunnels})
}

func (s *Server) handleTunnelDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET, OPTIONS")
		return
	}

	tunnelID, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/v1/tunnels/"))
	if err != nil || tunnelID == "" || strings.Contains(tunnelID, "/") {
		http.Error(w, "tunnel id is required", http.StatusBadRequest)
		return
	}

	session, ok := s.sessionByTunnelID(tunnelID)
	if !ok {
		http.Error(w, "tunnel not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, tunnelDetailResponse{Tunnel: s.tunnelDetail(session)})
}

func (s *Server) handleRequestLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET, OPTIONS")
		return
	}

	limit := requestLogLimit(r, s.cfg.RequestLogLimit)
	writeJSON(w, http.StatusOK, requestLogsResponse{RequestLogs: s.requestLogs.list(limit)})
}

func (s *Server) tunnelDetail(session *agentSession) tunnelDetail {
	connectedAt := session.tunnel.CreatedAt
	return tunnelDetail{
		TunnelID:         session.tunnel.TunnelID,
		Subdomain:        session.tunnel.Subdomain,
		PublicURL:        session.tunnel.PublicURL,
		Protocol:         session.tunnel.Protocol,
		AgentVersion:     session.tunnel.AgentVersion,
		ProtocolVersion:  session.tunnel.ProtocolVersion,
		ConnectedAt:      connectedAt,
		ConnectedSeconds: int64(time.Since(connectedAt).Seconds()),
		ActiveStreams:    session.activeStreams(),
		StreamCapacity:   session.streamCapacity(),
		RecentRequests:   s.tunnelRequestSummary(session.tunnel.TunnelID),
	}
}

func (s *Server) tunnelRequestSummary(tunnelID string) tunnelRequestSummary {
	logs := s.requestLogs.list(s.cfg.RequestLogLimit)
	summary := tunnelRequestSummary{}
	customDomains := make(map[string]struct{})
	for _, entry := range logs {
		if entry.TunnelID != tunnelID {
			continue
		}
		summary.Count++
		if summary.LastRequestAt == nil {
			requestAt := entry.Time
			summary.LastRequestAt = &requestAt
			summary.LastStatus = entry.Status
			summary.LastOutcome = entry.Outcome
			summary.LastRequestID = entry.RequestID
		}
		if entry.Status >= http.StatusInternalServerError {
			summary.ErrorCount++
		}
		if entry.CustomDomain != "" {
			customDomains[entry.CustomDomain] = struct{}{}
		}
	}
	if len(customDomains) > 0 {
		summary.CustomDomains = make([]string, 0, len(customDomains))
		for hostname := range customDomains {
			summary.CustomDomains = append(summary.CustomDomains, hostname)
		}
		sort.Strings(summary.CustomDomains)
	}
	return summary
}

type agentIdentity struct {
	agentTokenValidation
	AgentVersion    string
	ProtocolVersion string
	Capabilities    []string
}

func (s *Server) authenticateAgent(ctx context.Context, conn *websocket.Conn) (agentIdentity, error) {
	var env messages.Envelope
	if err := wsjson.Read(ctx, conn, &env); err != nil {
		return agentIdentity{}, fmt.Errorf("read auth request: %w", err)
	}
	if env.Type != messages.TypeAuthRequest {
		return agentIdentity{}, fmt.Errorf("expected %s, got %s", messages.TypeAuthRequest, env.Type)
	}

	payload, err := messages.DecodePayload[messages.AuthRequest](env)
	if err != nil {
		return agentIdentity{}, err
	}
	s.metrics.tokenValidationsTotal.Add(1)
	validation, err := s.tokens.ValidateAgentToken(ctx, payload.Token)
	if err != nil {
		s.metrics.tokenValidationErrorsTotal.Add(1)
		authErr, _ := messages.New(messages.TypeAuthError, messages.ErrorPayload{
			Code:    "auth_unavailable",
			Message: "token validation failed",
		})
		_ = wsjson.Write(ctx, conn, authErr)
		return agentIdentity{}, fmt.Errorf("validate token: %w", err)
	}
	if !validation.Valid {
		authErr, _ := messages.New(messages.TypeAuthError, messages.ErrorPayload{
			Code:    "invalid_token",
			Message: "invalid token",
		})
		_ = wsjson.Write(ctx, conn, authErr)
		return agentIdentity{}, errors.New("invalid token")
	}
	if payload.ProtocolVersion != messages.ProtocolVersion {
		authErr, _ := messages.New(messages.TypeAuthError, messages.ErrorPayload{
			Code:    "unsupported_protocol",
			Message: fmt.Sprintf("protocol version %q is not supported, expected %q", payload.ProtocolVersion, messages.ProtocolVersion),
		})
		_ = wsjson.Write(ctx, conn, authErr)
		return agentIdentity{}, fmt.Errorf("unsupported protocol version %q", payload.ProtocolVersion)
	}
	missing := messages.MissingRequiredCapabilities(payload.Capabilities, messages.DefaultProtocolCapabilities())
	if len(missing) > 0 {
		authErr, _ := messages.New(messages.TypeAuthError, messages.ErrorPayload{
			Code:    "unsupported_protocol",
			Message: fmt.Sprintf("agent protocol is missing capabilities: %s", strings.Join(missing, ", ")),
		})
		_ = wsjson.Write(ctx, conn, authErr)
		return agentIdentity{}, fmt.Errorf("agent capabilities missing: %v", strings.Join(missing, ", "))
	}

	authOK, err := messages.New(messages.TypeAuthOK, messages.AuthOK{
		ProtocolVersion: messages.ProtocolVersion,
		Capabilities:    messages.DefaultProtocolCapabilities(),
	})
	if err != nil {
		return agentIdentity{}, err
	}
	if err := wsjson.Write(ctx, conn, authOK); err != nil {
		return agentIdentity{}, err
	}
	return agentIdentity{
		agentTokenValidation: validation,
		AgentVersion:         payload.AgentVersion,
		ProtocolVersion:      payload.ProtocolVersion,
		Capabilities:         append([]string(nil), payload.Capabilities...),
	}, nil
}

func (s *Server) registerTunnel(ctx context.Context, conn *websocket.Conn, identity agentIdentity) (*agentSession, error) {
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

	tunnel, err := s.registerTunnelSession(ctx, conn, identity, payload)
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

	return newAgentSession(
		conn,
		tunnel,
		s.cfg.WebSocketWriteTimeout,
		s.cfg.MaxConcurrentStreams,
		s.cfg.RateLimitRequestsPerSecond,
		s.cfg.RateLimitBurst,
	), nil
}

func (s *Server) registerTunnelSession(ctx context.Context, conn *websocket.Conn, identity agentIdentity, payload messages.TunnelRegister) (*registry.Session, error) {
	if payload.RequestedSubdomain != "" {
		return s.registerRequestedSubdomain(ctx, conn, identity, payload)
	}
	return s.registerRandomSubdomain(ctx, conn, identity, payload)
}

func (s *Server) registerRequestedSubdomain(ctx context.Context, conn *websocket.Conn, identity agentIdentity, payload messages.TunnelRegister) (*registry.Session, error) {
	subdomain := payload.RequestedSubdomain
	if err := names.ValidateSubdomain(subdomain); err != nil {
		message := fmt.Sprintf("invalid requested subdomain %q: %s", subdomain, err)
		_ = writeTunnelError(ctx, conn, "invalid_subdomain", message)
		return nil, errors.New(message)
	}

	authorization, err := s.reserved.AuthorizeReservedSubdomain(ctx, identity.TokenID, subdomain)
	if err != nil {
		message := "reserved subdomain authorization failed"
		_ = writeTunnelError(ctx, conn, "subdomain_authorization_unavailable", message)
		return nil, fmt.Errorf("%s: %w", message, err)
	}
	if !authorization.Allowed {
		code, message := reservedSubdomainDeniedError(subdomain, authorization.Reason)
		_ = writeTunnelError(ctx, conn, code, message)
		return nil, errors.New(message)
	}

	tunnel, err := s.newTunnelSession(payload, identity, subdomain)
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

func (s *Server) registerRandomSubdomain(ctx context.Context, conn *websocket.Conn, identity agentIdentity, payload messages.TunnelRegister) (*registry.Session, error) {
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

		tunnel, err := s.newTunnelSession(payload, identity, subdomain)
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

func (s *Server) newTunnelSession(payload messages.TunnelRegister, identity agentIdentity, subdomain string) (*registry.Session, error) {
	tunnelID, err := s.generateTunnelID()
	if err != nil {
		return nil, err
	}

	return &registry.Session{
		TunnelID:        tunnelID,
		Subdomain:       subdomain,
		PublicURL:       s.publicURLForSubdomain(subdomain),
		LocalTarget:     payload.LocalTarget,
		Protocol:        payload.Protocol,
		AgentVersion:    identity.AgentVersion,
		ProtocolVersion: identity.ProtocolVersion,
		CreatedAt:       time.Now().UTC(),
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func reservedSubdomainDeniedError(subdomain, reason string) (string, string) {
	switch reason {
	case "not_reserved":
		return "subdomain_not_reserved", fmt.Sprintf("subdomain %q is not reserved for this token; reserve it first or omit --subdomain for a random subdomain", subdomain)
	case "reserved_for_another_token":
		return "subdomain_reserved", fmt.Sprintf("subdomain %q is reserved for another token; choose another name or omit --subdomain for a random subdomain", subdomain)
	case "missing_token_id":
		return "subdomain_authorization_unavailable", "control plane did not identify the agent token; omit --subdomain or upgrade the control plane"
	default:
		if strings.TrimSpace(reason) == "" {
			reason = "not allowed"
		}
		return "subdomain_not_allowed", fmt.Sprintf("subdomain %q is not allowed: %s", subdomain, reason)
	}
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
	var customDomain string
	var tunnelID string
	var streamID string
	var requestBytes int64
	var responseBytes int64
	var requestErr error
	var err error
	defer func() {
		if status == 0 {
			status = http.StatusInternalServerError
		}
		entry := publicRequestLog{
			Started:       started,
			Status:        status,
			Outcome:       outcome,
			Subdomain:     subdomain,
			CustomDomain:  customDomain,
			TunnelID:      tunnelID,
			StreamID:      streamID,
			RequestBytes:  requestBytes,
			ResponseBytes: responseBytes,
			Error:         requestErr,
		}
		s.logPublicRequest(r, entry)
		s.requestLogs.add(requestLogEntryFromPublicRequest(r, entry))
		s.recordPublicRequestMetrics(status, outcome)
		telemetry.RecordSpan(r.Context(), status, outcome, requestErr,
			attribute.String("porthook.subdomain", subdomain),
			attribute.String("porthook.custom_domain", customDomain),
			attribute.String("porthook.tunnel_id", tunnelID),
			attribute.String("porthook.stream_id", streamID),
			attribute.Int64("porthook.request_bytes", requestBytes),
			attribute.Int64("porthook.response_bytes", responseBytes),
		)
	}()

	route, ok, err := s.routeForHost(r.Context(), r.Host)
	if err != nil {
		status = http.StatusServiceUnavailable
		outcome = "custom_domain_lookup_failed"
		requestErr = err
		writePublicError(w, r, "custom domain lookup failed", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		status = http.StatusNotFound
		customDomain = route.CustomDomain
		if route.MissReason != "" {
			outcome = customDomainMissOutcome(route.MissReason)
			writePublicError(w, r, customDomainMissMessage(route.MissReason), http.StatusNotFound)
			return
		}
		outcome = "no_tunnel_for_host"
		writePublicError(w, r, fmt.Sprintf("no active tunnel for host %q", r.Host), http.StatusNotFound)
		return
	}
	subdomain = route.Subdomain
	customDomain = route.CustomDomain

	session, ok := s.sessionBySubdomain(subdomain)
	if !ok {
		status = http.StatusNotFound
		outcome = "no_active_session"
		writePublicError(w, r, fmt.Sprintf("no active tunnel for host %q", r.Host), http.StatusNotFound)
		return
	}
	tunnelID = session.tunnel.TunnelID

	if !session.allowRequest(time.Now()) {
		status = http.StatusTooManyRequests
		outcome = "rate_limited"
		w.Header().Set("Retry-After", "1")
		writePublicError(w, r, "tunnel rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	accessResult, err := s.evaluatePublicAccess(r, subdomain)
	if err != nil {
		status = http.StatusServiceUnavailable
		outcome = "access_policy_unavailable"
		requestErr = err
		writePublicError(w, r, "access policy unavailable", http.StatusServiceUnavailable)
		return
	}
	if !accessResult.Allowed {
		status = accessDeniedStatus(accessResult)
		outcome = "access_denied"
		writeAccessDenied(w, r, accessResult)
		return
	}

	streamID, err = newStreamID()
	if err != nil {
		status = http.StatusInternalServerError
		outcome = "stream_id_failed"
		requestErr = err
		writePublicError(w, r, "failed to create stream", http.StatusInternalServerError)
		return
	}

	requestHeader := httpwire.StripHopByHopHeaders(r.Header)
	if accessConsumesAuthorization(accessResult) {
		requestHeader.Del("Authorization")
	}
	requestHeader = httpwire.AddForwardedHeaders(
		requestHeader,
		session.tunnel.TunnelID,
		r.Host,
		requestProto(r),
		remoteIP(r.RemoteAddr),
	)

	contentLength := r.ContentLength
	if contentLength < 0 {
		contentLength = 0
	}
	tunnelRequest := httpwire.RequestStart{
		Method:        r.Method,
		Path:          r.URL.Path,
		Query:         r.URL.RawQuery,
		Header:        requestHeader,
		ContentLength: contentLength,
	}
	defer r.Body.Close()

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.StreamTimeout)
	defer cancel()

	responseStarted := false
	writeResponseStart := func(tunnelResponse httpwire.ResponseStart) {
		for name, values := range httpwire.StripHopByHopHeaders(tunnelResponse.Header) {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		if tunnelResponse.Status <= 0 {
			tunnelResponse.Status = http.StatusBadGateway
		}
		status = tunnelResponse.Status
		responseStarted = true
		w.WriteHeader(tunnelResponse.Status)
	}
	writeResponseBody := func(chunk []byte) (int, error) {
		n, err := w.Write(chunk)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return n, err
	}

	result, err := session.streamRoundTrip(
		ctx,
		streamID,
		tunnelRequest,
		r.Body,
		s.cfg.MaxBodyBytes,
		s.cfg.StreamChunkBytes,
		writeResponseStart,
		writeResponseBody,
	)
	requestBytes = result.RequestBytes
	responseBytes = result.ResponseBytes
	if err != nil {
		requestErr = err
		switch {
		case errors.Is(err, ErrStreamLimitExceeded):
			status = http.StatusServiceUnavailable
			outcome = "stream_limit_exceeded"
			writePublicError(w, r, "tunnel overloaded", http.StatusServiceUnavailable)
			return
		case errors.Is(err, ErrRequestBodyTooLarge):
			status = http.StatusRequestEntityTooLarge
			outcome = "request_body_too_large"
			writePublicError(w, r, "request body too large", http.StatusRequestEntityTooLarge)
			return
		case errors.Is(err, context.DeadlineExceeded):
			if !responseStarted {
				status = http.StatusGatewayTimeout
			}
			outcome = "tunnel_timeout"
			if !responseStarted {
				writePublicError(w, r, "tunnel request timed out", http.StatusGatewayTimeout)
			}
			return
		case errors.Is(err, context.Canceled):
			if !responseStarted {
				status = 499
			}
			outcome = "client_canceled"
			return
		}
		if !responseStarted {
			status = http.StatusBadGateway
		}
		outcome = "tunnel_error"
		if !responseStarted {
			writePublicError(w, r, "tunnel request failed", http.StatusBadGateway)
		}
		return
	}

	status = result.Status
	outcome = "completed"
}

func (s *Server) evaluatePublicAccess(r *http.Request, subdomain string) (accessPolicyEvaluation, error) {
	username, password, _ := r.BasicAuth()
	evaluation := accessPolicyEvaluationRequest{
		Subdomain:     subdomain,
		RemoteIP:      remoteIP(r.RemoteAddr),
		BasicUsername: username,
		BasicPassword: password,
		BearerToken:   bearerToken(r),
	}
	return s.access.EvaluateAccessPolicy(r.Context(), evaluation)
}

func bearerToken(r *http.Request) string {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

func accessDeniedStatus(result accessPolicyEvaluation) int {
	switch result.Mode {
	case "basic_auth", "bearer_token":
		return http.StatusUnauthorized
	default:
		return http.StatusForbidden
	}
}

func writeAccessDenied(w http.ResponseWriter, r *http.Request, result accessPolicyEvaluation) {
	switch result.Mode {
	case "basic_auth":
		w.Header().Set("WWW-Authenticate", `Basic realm="Porthook tunnel"`)
		writePublicError(w, r, "authentication required", http.StatusUnauthorized)
	case "bearer_token":
		w.Header().Set("WWW-Authenticate", `Bearer realm="Porthook tunnel"`)
		writePublicError(w, r, "bearer token required", http.StatusUnauthorized)
	default:
		writePublicError(w, r, "access denied", http.StatusForbidden)
	}
}

func writePublicError(w http.ResponseWriter, r *http.Request, message string, status int) {
	if r != nil {
		if id := requestID(r); id != "" {
			message = fmt.Sprintf("%s\nrequest_id: %s", message, id)
		}
	}
	http.Error(w, message, status)
}

func accessConsumesAuthorization(result accessPolicyEvaluation) bool {
	return result.Allowed && (result.Mode == "basic_auth" || result.Mode == "bearer_token")
}

type publicRequestLog struct {
	Started       time.Time
	Status        int
	Outcome       string
	Subdomain     string
	CustomDomain  string
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
		slog.String("event", "gateway.public_request"),
		slog.String("method", r.Method),
		slog.String("host", r.Host),
		slog.String("path", r.URL.Path),
		slog.Bool("query_present", r.URL.RawQuery != ""),
		slog.String("remote_ip", remoteIP(r.RemoteAddr)),
		slog.String("subdomain", entry.Subdomain),
		slog.String("custom_domain", entry.CustomDomain),
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
	if id := requestID(r); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}

	s.logger.LogAttrs(r.Context(), level, "public request", attrs...)
}

type hostRoute struct {
	Subdomain    string
	CustomDomain string
	MissReason   string
}

func (s *Server) routeForHost(ctx context.Context, hostport string) (hostRoute, bool, error) {
	if subdomain, ok := s.subdomainFromHost(hostport); ok {
		return hostRoute{Subdomain: subdomain}, true, nil
	}

	hostname, ok := customDomainLookupHostname(hostport)
	if !ok {
		return hostRoute{}, false, nil
	}
	if sameHostname(hostname, s.cfg.RootDomain) {
		return hostRoute{}, false, nil
	}
	s.metrics.customDomainLookupsTotal.Add(1)
	result, err := s.domains.ResolveCustomDomain(ctx, hostname)
	if err != nil {
		s.metrics.customDomainLookupErrorsTotal.Add(1)
		return hostRoute{}, false, err
	}
	resultHostname := strings.TrimSpace(result.Hostname)
	if resultHostname == "" {
		resultHostname = hostname
	}
	if !result.Found {
		s.metrics.customDomainLookupMissesTotal.Add(1)
		return hostRoute{CustomDomain: resultHostname, MissReason: customDomainMissReason(result.Reason)}, false, nil
	}
	if result.Status != "" && result.Status != "active" {
		s.metrics.customDomainLookupMissesTotal.Add(1)
		return hostRoute{CustomDomain: resultHostname, MissReason: customDomainStatusMissReason(result.Status)}, false, nil
	}
	subdomain := strings.ToLower(strings.TrimSpace(result.Subdomain))
	if subdomain == "" {
		s.metrics.customDomainLookupErrorsTotal.Add(1)
		return hostRoute{}, false, fmt.Errorf("custom domain %q did not resolve to a subdomain", hostname)
	}
	if err := names.ValidateSubdomain(subdomain); err != nil {
		s.metrics.customDomainLookupErrorsTotal.Add(1)
		return hostRoute{}, false, fmt.Errorf("custom domain %q resolved to invalid subdomain %q: %w", hostname, result.Subdomain, err)
	}
	s.metrics.customDomainLookupHitsTotal.Add(1)
	return hostRoute{Subdomain: subdomain, CustomDomain: resultHostname}, true, nil
}

func sameHostname(left, right string) bool {
	left = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(left)), ".")
	right = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(right)), ".")
	return left != "" && left == right
}

func customDomainStatusMissReason(status string) string {
	switch strings.TrimSpace(status) {
	case "pending_verification":
		return "not_verified"
	case "verification_failed":
		return "verification_failed"
	default:
		return "not_found"
	}
}

func customDomainMissReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "not_found"
	}
	return reason
}

func customDomainMissOutcome(reason string) string {
	switch strings.TrimSpace(reason) {
	case "not_verified":
		return "custom_domain_not_verified"
	case "verification_failed":
		return "custom_domain_verification_failed"
	case "no_control_plane":
		return "custom_domain_no_control_plane"
	case "not_found", "":
		return "custom_domain_not_found"
	default:
		return "custom_domain_lookup_miss"
	}
}

func customDomainMissMessage(reason string) string {
	switch strings.TrimSpace(reason) {
	case "not_verified":
		return "custom domain is pending verification"
	case "verification_failed":
		return "custom domain verification failed"
	case "no_control_plane":
		return "custom domains require a control plane"
	default:
		return "custom domain was not found"
	}
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

func customDomainLookupHostname(hostport string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(hostport))
	if host == "" {
		return "", false
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	} else if strings.Contains(host, ":") {
		return "", false
	}
	host = strings.TrimSuffix(host, ".")
	if !strings.Contains(host, ".") {
		return "", false
	}
	if strings.ContainsAny(host, "*/_") {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > names.MaxSubdomainLength || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= '0' && r <= '9':
			case r == '-':
			default:
				return "", false
			}
		}
	}
	return host, true
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

func (s *Server) sessionByTunnelID(tunnelID string) (*agentSession, bool) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	session, ok := s.sessions[tunnelID]
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
	s.logger.Info("tunnel disconnected", "event", "gateway.tunnel_disconnected", "tunnel_id", tunnelID)
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
				s.logger.Warn("close tunnel session failed", "event", "gateway.tunnel_close_failed", "tunnel_id", session.tunnel.TunnelID, "error", err)
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
				s.logger.Warn("force close tunnel session failed", "event", "gateway.tunnel_force_close_failed", "tunnel_id", session.tunnel.TunnelID, "error", err)
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

func requestAuditAttrs(r *http.Request, event string) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("event", event),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("remote_ip", remoteIP(r.RemoteAddr)),
	}
	if id := requestID(r); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	return attrs
}

func requestID(r *http.Request) string {
	return requestid.FromRequest(r)
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
