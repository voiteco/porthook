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
	"github.com/voiteco/porthook/server/internal/clientip"
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
	cfg             Config
	logger          *slog.Logger
	registry        *registry.Registry
	tokens          agentTokenValidator
	reserved        reservedSubdomainAuthorizer
	access          accessPolicyEvaluator
	domains         customDomainResolver
	metrics         metrics
	startedAt       time.Time
	requestLogs     *requestLogBuffer
	requestLogStore requestLogWriter
	clientIPs       clientip.Resolver
	authAttempts    *authAttemptThrottle

	sessionsMu sync.RWMutex
	sessions   map[string]*agentSession

	generateSubdomain func(length int) (string, error)
	generateTunnelID  func() (string, error)
}

func NewServer(cfg Config, logger *slog.Logger) *Server {
	return newServerWithRequestLogWriter(cfg, logger, nil)
}

func NewServerWithRequestLogStore(cfg Config, logger *slog.Logger, requestLogStore *PostgresRequestLogStore) *Server {
	if requestLogStore == nil {
		return newServerWithRequestLogWriter(cfg, logger, nil)
	}
	return newServerWithRequestLogWriter(cfg, logger, requestLogStore)
}

func newServerWithRequestLogWriter(cfg Config, logger *slog.Logger, requestLogStore requestLogWriter) *Server {
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
	trustedProxies, err := clientip.ParseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		logger.Warn("gateway trusted proxy configuration failed", "event", "gateway.trusted_proxy_config_failed", "error", err)
		trustedProxies = nil
	}

	return &Server{
		cfg:             cfg,
		logger:          logger,
		registry:        registry.New(),
		tokens:          tokenValidator,
		reserved:        reservedAuthorizer,
		access:          accessPolicyEvaluator,
		domains:         customDomainResolver,
		metrics:         newMetrics(),
		startedAt:       time.Now().UTC(),
		requestLogs:     newRequestLogBuffer(cfg.RequestLogLimit),
		requestLogStore: requestLogStore,
		clientIPs:       clientip.New(trustedProxies),
		authAttempts:    newAuthAttemptThrottle(cfg.AuthAttemptLimit, cfg.AuthAttemptWindow),
		sessions:        make(map[string]*agentSession),

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
		// WriteTimeout is deliberately unset: a fixed connection-level
		// write deadline would kill long-lived SSE, long-polling, and
		// WebSocket tunnel responses regardless of activity. Response
		// duration is bounded instead by the stream-level request, idle,
		// and max-lifetime policy applied per request in handlePublicRequest
		// and the public WebSocket relay.
		IdleTimeout: s.cfg.IdleTimeout,
	}
	agentServer := &http.Server{
		Addr:              s.cfg.AgentAddr,
		Handler:           s.AgentHandler(),
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}
	managementServer := &http.Server{
		Addr:              s.cfg.ManagementAddr,
		Handler:           s.ManagementHandler(),
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}

	errCh := make(chan error, 3)
	go serveHTTP(errCh, publicServer)
	go serveHTTP(errCh, agentServer)
	go serveHTTP(errCh, managementServer)

	stopRequestLogPruner := s.startRequestLogPruner(ctx)
	defer stopRequestLogPruner()

	s.logger.Info("gateway started", "event", "gateway.started", "public_addr", s.cfg.PublicAddr, "agent_addr", s.cfg.AgentAddr, "management_addr", s.cfg.ManagementAddr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		s.closeSessions(shutdownCtx, websocket.StatusGoingAway, "gateway shutting down")
		errPublic := publicServer.Shutdown(shutdownCtx)
		errAgent := agentServer.Shutdown(shutdownCtx)
		errManagement := managementServer.Shutdown(shutdownCtx)
		return errors.Join(errPublic, errAgent, errManagement)
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		s.closeSessions(shutdownCtx, websocket.StatusInternalError, "gateway listener stopped")
		errPublic := publicServer.Shutdown(shutdownCtx)
		errAgent := agentServer.Shutdown(shutdownCtx)
		errManagement := managementServer.Shutdown(shutdownCtx)
		return errors.Join(err, errPublic, errAgent, errManagement)
	}
}

func (s *Server) startRequestLogPruner(ctx context.Context) func() {
	if s.cfg.RequestLogRetention <= 0 || s.cfg.RequestLogPruneInterval <= 0 {
		return func() {}
	}
	pruner := requestLogPruner(s.requestLogs)
	if storePruner, ok := s.requestLogStore.(requestLogPruner); ok {
		pruner = storePruner
	}
	if pruner == nil {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(s.cfg.RequestLogPruneInterval)
		defer ticker.Stop()
		s.pruneRequestLogs(ctx, pruner)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				s.pruneRequestLogs(ctx, pruner)
			}
		}
	}()
	return func() {
		close(done)
	}
}

func (s *Server) pruneRequestLogs(ctx context.Context, pruner requestLogPruner) {
	cutoff := time.Now().UTC().Add(-s.cfg.RequestLogRetention)
	count, err := pruner.PruneBefore(ctx, cutoff)
	if err != nil {
		s.logger.WarnContext(ctx, "request log pruning failed",
			slog.String("event", "gateway.request_logs_prune_failed"),
			slog.String("error", err.Error()),
		)
		return
	}
	if count > 0 {
		s.logger.InfoContext(ctx, "request logs pruned",
			slog.String("event", "gateway.request_logs_pruned"),
			slog.Int64("count", count),
			slog.Time("cutoff", cutoff),
		)
	}
}

func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handlePublicRequest)
	return requestid.Middleware(telemetry.HTTPHandler(s.withClientIPTrace(mux), "gateway.public"))
}

func (s *Server) AgentHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(agentWebSocketPath, s.handleAgentWebSocket)
	return requestid.Middleware(telemetry.HTTPHandler(s.withClientIPTrace(mux), "gateway.agent"))
}

func (s *Server) ManagementHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.ready(r.Context()); err != nil {
			http.Error(w, "not ready: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.HandleFunc("/api/v1/tunnels/", s.requireManagementAuth(s.handleTunnelDetail))
	mux.HandleFunc("/api/v1/tunnels", s.requireManagementAuth(s.handleTunnelList))
	mux.HandleFunc("/api/v1/runtime", s.requireManagementAuth(s.handleRuntimeSummary))
	mux.HandleFunc("/api/v1/request-logs", s.requireManagementAuth(s.handleRequestLogs))
	mux.HandleFunc("/metrics", s.requireManagementAuth(s.handleMetrics))
	return requestid.Middleware(telemetry.HTTPHandler(s.withClientIPTrace(mux), "gateway.management"))
}

func (s *Server) requireManagementAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.ManagementToken == "" {
			next(w, r)
			return
		}
		if !secretEqual(bearerToken(r), s.cfg.ManagementToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Porthook gateway management"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		attrs := s.requestAuditAttrs(r, "gateway.agent_websocket_accept_failed")
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
		attrs := s.requestAuditAttrs(r, "gateway.agent_auth_failed")
		attrs = append(attrs, slog.Any("error", err))
		s.logger.LogAttrs(r.Context(), slog.LevelWarn, "agent authentication failed", attrs...)
		_ = conn.Close(websocket.StatusPolicyViolation, "authentication failed")
		return
	}

	session, err := s.registerTunnel(ctx, conn, identity)
	if err != nil {
		s.metrics.tunnelRegistrationFailuresTotal.Add(1)
		attrs := s.requestAuditAttrs(r, "gateway.tunnel_registration_failed")
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

type runtimeSummaryResponse struct {
	Runtime runtimeSummary `json:"runtime"`
}

type runtimeSummary struct {
	StartedAt              time.Time       `json:"started_at"`
	UptimeSeconds          int64           `json:"uptime_seconds"`
	RootDomain             string          `json:"root_domain"`
	PublicURL              string          `json:"public_url"`
	ControlPlaneConfigured bool            `json:"control_plane_configured"`
	ActiveTunnels          int             `json:"active_tunnels"`
	ActiveStreams          int             `json:"active_streams"`
	StreamCapacity         int             `json:"stream_capacity"`
	RequestLogEntries      int             `json:"request_log_entries"`
	RequestLogCapacity     int             `json:"request_log_capacity"`
	Limits                 runtimeLimits   `json:"limits"`
	Timeouts               runtimeTimeouts `json:"timeouts"`
	Counters               runtimeCounters `json:"counters"`
}

type runtimeLimits struct {
	MaxBodyBytes               int64 `json:"max_body_bytes"`
	MaxConcurrentStreams       int   `json:"max_concurrent_streams"`
	RateLimitRequestsPerSecond int   `json:"rate_limit_rps"`
	RateLimitBurst             int   `json:"rate_limit_burst"`
	AuthAttemptLimit           int   `json:"auth_attempt_limit"`
	AuthAttemptWindowSeconds   int64 `json:"auth_attempt_window_seconds"`
	StreamChunkBytes           int   `json:"stream_chunk_bytes"`
}

type runtimeTimeouts struct {
	ControlPlaneTimeoutSeconds   int64 `json:"control_plane_timeout_seconds"`
	ReadHeaderTimeoutSeconds     int64 `json:"read_header_timeout_seconds"`
	ReadTimeoutSeconds           int64 `json:"read_timeout_seconds"`
	WriteTimeoutSeconds          int64 `json:"write_timeout_seconds"`
	IdleTimeoutSeconds           int64 `json:"idle_timeout_seconds"`
	HandshakeTimeoutSeconds      int64 `json:"handshake_timeout_seconds"`
	StreamRequestTimeoutSeconds  int64 `json:"stream_request_timeout_seconds"`
	StreamIdleTimeoutSeconds     int64 `json:"stream_idle_timeout_seconds"`
	StreamMaxLifetimeSeconds     int64 `json:"stream_max_lifetime_seconds"`
	WebSocketWriteTimeoutSeconds int64 `json:"websocket_write_timeout_seconds"`
	WebSocketPingIntervalSeconds int64 `json:"websocket_ping_interval_seconds"`
	WebSocketPongTimeoutSeconds  int64 `json:"websocket_pong_timeout_seconds"`
	ShutdownTimeoutSeconds       int64 `json:"shutdown_timeout_seconds"`
}

type runtimeCounters struct {
	PublicRequestsTotal                   uint64 `json:"public_requests_total"`
	PublicRequestErrorsTotal              uint64 `json:"public_request_errors_total"`
	PublicRequestRateLimitedTotal         uint64 `json:"public_request_rate_limited_total"`
	PublicRequestAuthAttemptsLimitedTotal uint64 `json:"public_request_auth_attempts_limited_total"`
	PublicRequestTimeoutsTotal            uint64 `json:"public_request_timeouts_total"`
	PublicRequestStreamIdleTimeoutsTotal  uint64 `json:"public_request_stream_idle_timeouts_total"`
	PublicRequestStreamMaxLifetimeTotal   uint64 `json:"public_request_stream_max_lifetime_total"`
	PublicRequestBodyTooLargeTotal        uint64 `json:"public_request_body_too_large_total"`
	PublicRequestClientCanceledTotal      uint64 `json:"public_request_client_canceled_total"`
	PublicRequestNoActiveSessionTotal     uint64 `json:"public_request_no_active_session_total"`
	PublicRequestAccessDeniedTotal        uint64 `json:"public_request_access_denied_total"`
	PublicRequestAccessPolicyErrorsTotal  uint64 `json:"public_request_access_policy_errors_total"`
	CustomDomainLookupsTotal              uint64 `json:"custom_domain_lookups_total"`
	CustomDomainLookupHitsTotal           uint64 `json:"custom_domain_lookup_hits_total"`
	CustomDomainLookupMissesTotal         uint64 `json:"custom_domain_lookup_misses_total"`
	CustomDomainLookupErrorsTotal         uint64 `json:"custom_domain_lookup_errors_total"`
	TokenValidationsTotal                 uint64 `json:"token_validations_total"`
	TokenValidationErrorsTotal            uint64 `json:"token_validation_errors_total"`
	AuthFailuresTotal                     uint64 `json:"auth_failures_total"`
	TunnelRegistrationsTotal              uint64 `json:"tunnel_registrations_total"`
	TunnelRegistrationFailuresTotal       uint64 `json:"tunnel_registration_failures_total"`
}

func (s *Server) handleTunnelList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
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
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
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

func (s *Server) handleRuntimeSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	writeJSON(w, http.StatusOK, runtimeSummaryResponse{Runtime: s.runtimeSummary()})
}

func (s *Server) handleRequestLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	opts, err := requestLogListOptionsFromRequest(r, s.cfg.RequestLogLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := s.listRequestLogs(r.Context(), opts)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "list request logs failed", "error", err)
		http.Error(w, "list request logs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, requestLogsResponse{
		RequestLogs: page.RequestLogs,
		NextCursor:  page.NextCursor,
		Filters:     requestLogFiltersForResponse(opts, strings.TrimSpace(r.URL.Query().Get("cursor"))),
	})
}

func (s *Server) runtimeSummary() runtimeSummary {
	activeStreams, streamCapacity := s.streamStats()
	return runtimeSummary{
		StartedAt:              s.startedAt,
		UptimeSeconds:          int64(time.Since(s.startedAt).Seconds()),
		RootDomain:             s.cfg.RootDomain,
		PublicURL:              s.cfg.PublicURL,
		ControlPlaneConfigured: s.cfg.ControlPlaneURL != "",
		ActiveTunnels:          s.registry.Count(),
		ActiveStreams:          activeStreams,
		StreamCapacity:         streamCapacity,
		RequestLogEntries:      s.requestLogs.count(),
		RequestLogCapacity:     s.requestLogs.capacity(),
		Limits: runtimeLimits{
			MaxBodyBytes:               s.cfg.MaxBodyBytes,
			MaxConcurrentStreams:       s.cfg.MaxConcurrentStreams,
			RateLimitRequestsPerSecond: s.cfg.RateLimitRequestsPerSecond,
			RateLimitBurst:             s.cfg.RateLimitBurst,
			AuthAttemptLimit:           s.cfg.AuthAttemptLimit,
			AuthAttemptWindowSeconds:   int64(s.cfg.AuthAttemptWindow.Seconds()),
			StreamChunkBytes:           s.cfg.StreamChunkBytes,
		},
		Timeouts: runtimeTimeouts{
			ControlPlaneTimeoutSeconds:   int64(s.cfg.ControlPlaneTimeout.Seconds()),
			ReadHeaderTimeoutSeconds:     int64(s.cfg.ReadHeaderTimeout.Seconds()),
			ReadTimeoutSeconds:           int64(s.cfg.ReadTimeout.Seconds()),
			WriteTimeoutSeconds:          int64(s.cfg.WriteTimeout.Seconds()),
			IdleTimeoutSeconds:           int64(s.cfg.IdleTimeout.Seconds()),
			HandshakeTimeoutSeconds:      int64(s.cfg.HandshakeTimeout.Seconds()),
			StreamRequestTimeoutSeconds:  int64(s.cfg.StreamRequestTimeout.Seconds()),
			StreamIdleTimeoutSeconds:     int64(s.cfg.StreamIdleTimeout.Seconds()),
			StreamMaxLifetimeSeconds:     int64(s.cfg.StreamMaxLifetime.Seconds()),
			WebSocketWriteTimeoutSeconds: int64(s.cfg.WebSocketWriteTimeout.Seconds()),
			WebSocketPingIntervalSeconds: int64(s.cfg.WebSocketPingInterval.Seconds()),
			WebSocketPongTimeoutSeconds:  int64(s.cfg.WebSocketPongTimeout.Seconds()),
			ShutdownTimeoutSeconds:       int64(s.cfg.ShutdownTimeout.Seconds()),
		},
		Counters: runtimeCounters{
			PublicRequestsTotal:                   s.metrics.publicRequestsTotal.Load(),
			PublicRequestErrorsTotal:              s.metrics.publicRequestErrorsTotal.Load(),
			PublicRequestRateLimitedTotal:         s.metrics.publicRequestRateLimitedTotal.Load(),
			PublicRequestAuthAttemptsLimitedTotal: s.metrics.publicRequestAuthAttemptsLimitedTotal.Load(),
			PublicRequestTimeoutsTotal:            s.metrics.publicRequestTimeoutsTotal.Load(),
			PublicRequestStreamIdleTimeoutsTotal:  s.metrics.publicRequestStreamIdleTimeoutsTotal.Load(),
			PublicRequestStreamMaxLifetimeTotal:   s.metrics.publicRequestStreamMaxLifetimeTotal.Load(),
			PublicRequestBodyTooLargeTotal:        s.metrics.publicRequestBodyTooLargeTotal.Load(),
			PublicRequestClientCanceledTotal:      s.metrics.publicRequestClientCanceledTotal.Load(),
			PublicRequestNoActiveSessionTotal:     s.metrics.publicRequestNoActiveSessionTotal.Load(),
			PublicRequestAccessDeniedTotal:        s.metrics.publicRequestAccessDeniedTotal.Load(),
			PublicRequestAccessPolicyErrorsTotal:  s.metrics.publicRequestAccessPolicyErrorsTotal.Load(),
			CustomDomainLookupsTotal:              s.metrics.customDomainLookupsTotal.Load(),
			CustomDomainLookupHitsTotal:           s.metrics.customDomainLookupHitsTotal.Load(),
			CustomDomainLookupMissesTotal:         s.metrics.customDomainLookupMissesTotal.Load(),
			CustomDomainLookupErrorsTotal:         s.metrics.customDomainLookupErrorsTotal.Load(),
			TokenValidationsTotal:                 s.metrics.tokenValidationsTotal.Load(),
			TokenValidationErrorsTotal:            s.metrics.tokenValidationErrorsTotal.Load(),
			AuthFailuresTotal:                     s.metrics.authFailuresTotal.Load(),
			TunnelRegistrationsTotal:              s.metrics.tunnelRegistrationsTotal.Load(),
			TunnelRegistrationFailuresTotal:       s.metrics.tunnelRegistrationFailuresTotal.Load(),
		},
	}
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
	if !messages.IsProtocolVersionSupported(payload.ProtocolVersion) {
		authErr, _ := messages.New(messages.TypeAuthError, messages.ErrorPayload{
			Code:    "unsupported_protocol",
			Message: fmt.Sprintf("protocol version %q is not supported, minimum supported version is %q", payload.ProtocolVersion, messages.MinSupportedProtocolVersion),
		})
		_ = wsjson.Write(ctx, conn, authErr)
		return agentIdentity{}, fmt.Errorf("unsupported protocol version %q", payload.ProtocolVersion)
	}
	missing := messages.MissingRequiredCapabilities(payload.Capabilities, messages.RequiredProtocolCapabilities())
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
		Capabilities:    identity.Capabilities,
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
		logEntry := requestLogEntryFromPublicRequest(r, entry, s.clientIP(r))
		s.logPublicRequest(r, entry)
		s.recordRequestLog(logEntry)
		s.recordPublicRequestMetrics(status, outcome, time.Since(started))
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

	authAttemptKey := subdomain + "|" + s.clientIP(r)
	if s.authAttempts.blocked(authAttemptKey, started) {
		status = http.StatusTooManyRequests
		outcome = "auth_attempts_limited"
		w.Header().Set("Retry-After", "1")
		writePublicError(w, r, "too many authentication attempts", http.StatusTooManyRequests)
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
		if accessResult.Mode == "basic_auth" || accessResult.Mode == "bearer_token" {
			s.authAttempts.recordFailure(authAttemptKey, started)
		}
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

	deadline := newStreamDeadline(r.Context(), s.streamDeadlinePolicy())
	defer deadline.Stop()

	if isWebSocketUpgradeRequest(r) {
		status, outcome, requestBytes, responseBytes, requestErr = s.handlePublicWebSocket(w, r, session, accessResult, streamID, deadline)
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
		s.clientIP(r),
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

	ctx := deadline.Context()

	responseStarted := false
	writeResponseStart := func(tunnelResponse httpwire.ResponseStart) {
		deadline.MarkResponseStarted()
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
		deadline.Touch()
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
		case errors.Is(err, ErrStreamRequestTimeout), errors.Is(err, context.DeadlineExceeded):
			if !responseStarted {
				status = http.StatusGatewayTimeout
			}
			outcome = "tunnel_timeout"
			if !responseStarted {
				writePublicError(w, r, "tunnel request timed out", http.StatusGatewayTimeout)
			}
			return
		case errors.Is(err, ErrStreamIdleTimeout):
			if !responseStarted {
				status = http.StatusGatewayTimeout
			}
			outcome = "stream_idle_timeout"
			if !responseStarted {
				writePublicError(w, r, "tunnel stream idle timeout", http.StatusGatewayTimeout)
			}
			return
		case errors.Is(err, ErrStreamMaxLifetimeExceeded):
			if !responseStarted {
				status = http.StatusGatewayTimeout
			}
			outcome = "stream_max_lifetime_exceeded"
			if !responseStarted {
				writePublicError(w, r, "tunnel stream exceeded its maximum lifetime", http.StatusGatewayTimeout)
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

func (s *Server) streamDeadlinePolicy() streamDeadlinePolicy {
	return streamDeadlinePolicy{
		RequestTimeout: s.cfg.StreamRequestTimeout,
		IdleTimeout:    s.cfg.StreamIdleTimeout,
		MaxLifetime:    s.cfg.StreamMaxLifetime,
	}
}

func (s *Server) evaluatePublicAccess(r *http.Request, subdomain string) (accessPolicyEvaluation, error) {
	username, password, _ := r.BasicAuth()
	evaluation := accessPolicyEvaluationRequest{
		Subdomain:     subdomain,
		RemoteIP:      s.clientIP(r),
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

func (s *Server) ready(ctx context.Context) error {
	if s.requestLogStore == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RequestLogWriteTimeout)
	defer cancel()
	if err := s.requestLogStore.Ping(ctx); err != nil {
		return fmt.Errorf("request log store: %w", err)
	}
	return nil
}

func (s *Server) recordRequestLog(entry requestLogEntry) {
	s.requestLogs.add(entry)
	if s.requestLogStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.RequestLogWriteTimeout)
	defer cancel()
	if err := s.requestLogStore.Add(ctx, entry); err != nil {
		s.logger.Warn("request log store write failed",
			"event", "gateway.request_log_store_write_failed",
			"error", err,
			"request_id", entry.RequestID,
			"host", entry.Host,
			"path", entry.Path,
			"status", entry.Status,
			"outcome", entry.Outcome,
		)
	}
}

func (s *Server) listRequestLogs(ctx context.Context, opts requestLogListOptions) (requestLogListPage, error) {
	if reader, ok := s.requestLogStore.(requestLogReader); ok {
		return reader.List(ctx, opts)
	}
	return s.requestLogs.listPage(opts), nil
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
		slog.String("remote_ip", s.clientIP(r)),
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

func (s *Server) streamStats() (activeStreams, streamCapacity int) {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	for _, session := range s.sessions {
		activeStreams += session.activeStreams()
		streamCapacity += session.streamCapacity()
	}
	return activeStreams, streamCapacity
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

func (s *Server) clientIP(r *http.Request) string {
	return s.clientIPs.ResolveString(r)
}

func (s *Server) withClientIPTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		telemetry.SetSpanAttributes(r.Context(), attribute.String("porthook.client_ip", s.clientIP(r)))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestAuditAttrs(r *http.Request, event string) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("event", event),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("remote_ip", s.clientIP(r)),
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
