// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultPublicAddr              = ":8080"
	defaultAgentAddr               = ":8081"
	defaultManagementAddr          = ":8082"
	defaultRootDomain              = "localhost"
	defaultPublicURL               = "http://localhost:8080"
	defaultStaticToken             = "dev-token"
	defaultControlPlaneTimeout     = 5 * time.Second
	defaultCustomDomainCacheTTL    = 30 * time.Second
	defaultCustomDomainMissTTL     = 5 * time.Second
	defaultMaxBodyBytes            = 1 << 20
	defaultMaxConcurrentStreams    = 64
	defaultRateLimitRPS            = 60
	defaultRateLimitBurst          = 120
	defaultStreamChunkBytes        = 32 << 10
	defaultReadHeaderTimeout       = 5 * time.Second
	defaultReadTimeout             = 30 * time.Second
	defaultWriteTimeout            = 35 * time.Second
	defaultIdleTimeout             = 120 * time.Second
	defaultHandshakeTimeout        = 10 * time.Second
	defaultStreamTimeout           = 30 * time.Second
	defaultWebSocketWriteTimeout   = 10 * time.Second
	defaultWebSocketPingInterval   = 15 * time.Second
	defaultWebSocketPongTimeout    = 5 * time.Second
	defaultShutdownTimeout         = 5 * time.Second
	defaultRequestLogLimit         = 500
	defaultRequestLogWriteTimeout  = 1 * time.Second
	defaultRequestLogRetention     = 30 * 24 * time.Hour
	defaultRequestLogPruneInterval = time.Hour
)

type Config struct {
	PublicAddr                 string
	AgentAddr                  string
	ManagementAddr             string
	ManagementToken            string
	TrustedProxies             string
	RootDomain                 string
	PublicURL                  string
	StaticToken                string
	ControlPlaneURL            string
	ControlPlaneToken          string
	ControlPlaneTimeout        time.Duration
	CustomDomainCacheTTL       time.Duration
	CustomDomainMissTTL        time.Duration
	MaxBodyBytes               int64
	MaxConcurrentStreams       int
	RateLimitRequestsPerSecond int
	RateLimitBurst             int
	StreamChunkBytes           int
	ReadHeaderTimeout          time.Duration
	ReadTimeout                time.Duration
	WriteTimeout               time.Duration
	IdleTimeout                time.Duration
	HandshakeTimeout           time.Duration
	StreamTimeout              time.Duration
	WebSocketWriteTimeout      time.Duration
	WebSocketPingInterval      time.Duration
	WebSocketPongTimeout       time.Duration
	ShutdownTimeout            time.Duration
	RequestLogLimit            int
	RequestLogDatabaseURL      string
	RequestLogWriteTimeout     time.Duration
	RequestLogRetention        time.Duration
	RequestLogPruneInterval    time.Duration
}

func ConfigFromEnv() Config {
	return normalizeConfig(Config{
		PublicAddr:                 envString("PORTHOOK_ADDR", defaultPublicAddr),
		AgentAddr:                  envString("PORTHOOK_AGENT_ADDR", defaultAgentAddr),
		ManagementAddr:             envString("PORTHOOK_MANAGEMENT_ADDR", defaultManagementAddr),
		ManagementToken:            envString("PORTHOOK_MANAGEMENT_TOKEN", ""),
		TrustedProxies:             envString("PORTHOOK_TRUSTED_PROXIES", ""),
		RootDomain:                 envString("PORTHOOK_ROOT_DOMAIN", defaultRootDomain),
		PublicURL:                  envString("PORTHOOK_PUBLIC_URL", defaultPublicURL),
		StaticToken:                envString("PORTHOOK_STATIC_TOKEN", defaultStaticToken),
		ControlPlaneURL:            envString("PORTHOOK_CONTROL_PLANE_URL", ""),
		ControlPlaneToken:          envString("PORTHOOK_CONTROL_PLANE_TOKEN", ""),
		ControlPlaneTimeout:        envDuration("PORTHOOK_CONTROL_PLANE_TIMEOUT", defaultControlPlaneTimeout),
		CustomDomainCacheTTL:       envDuration("PORTHOOK_CUSTOM_DOMAIN_CACHE_TTL", defaultCustomDomainCacheTTL),
		CustomDomainMissTTL:        envDuration("PORTHOOK_CUSTOM_DOMAIN_MISS_TTL", defaultCustomDomainMissTTL),
		MaxBodyBytes:               envInt64("PORTHOOK_MAX_BODY_BYTES", defaultMaxBodyBytes),
		MaxConcurrentStreams:       envInt("PORTHOOK_MAX_CONCURRENT_STREAMS", defaultMaxConcurrentStreams),
		RateLimitRequestsPerSecond: envInt("PORTHOOK_RATE_LIMIT_RPS", defaultRateLimitRPS),
		RateLimitBurst:             envInt("PORTHOOK_RATE_LIMIT_BURST", defaultRateLimitBurst),
		StreamChunkBytes:           envInt("PORTHOOK_STREAM_CHUNK_BYTES", defaultStreamChunkBytes),
		ReadHeaderTimeout:          envDuration("PORTHOOK_READ_HEADER_TIMEOUT", defaultReadHeaderTimeout),
		ReadTimeout:                envDuration("PORTHOOK_READ_TIMEOUT", defaultReadTimeout),
		WriteTimeout:               envDuration("PORTHOOK_WRITE_TIMEOUT", defaultWriteTimeout),
		IdleTimeout:                envDuration("PORTHOOK_IDLE_TIMEOUT", defaultIdleTimeout),
		HandshakeTimeout:           envDuration("PORTHOOK_HANDSHAKE_TIMEOUT", defaultHandshakeTimeout),
		StreamTimeout:              envDuration("PORTHOOK_STREAM_TIMEOUT", defaultStreamTimeout),
		WebSocketWriteTimeout:      envDuration("PORTHOOK_WS_WRITE_TIMEOUT", defaultWebSocketWriteTimeout),
		WebSocketPingInterval:      envDuration("PORTHOOK_WS_PING_INTERVAL", defaultWebSocketPingInterval),
		WebSocketPongTimeout:       envDuration("PORTHOOK_WS_PONG_TIMEOUT", defaultWebSocketPongTimeout),
		ShutdownTimeout:            envDuration("PORTHOOK_SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		RequestLogLimit:            envInt("PORTHOOK_REQUEST_LOG_LIMIT", defaultRequestLogLimit),
		RequestLogDatabaseURL:      envString("PORTHOOK_REQUEST_LOG_DATABASE_URL", ""),
		RequestLogWriteTimeout:     envDuration("PORTHOOK_REQUEST_LOG_WRITE_TIMEOUT", defaultRequestLogWriteTimeout),
		RequestLogRetention:        envDuration("PORTHOOK_REQUEST_LOG_RETENTION", defaultRequestLogRetention),
		RequestLogPruneInterval:    envDuration("PORTHOOK_REQUEST_LOG_PRUNE_INTERVAL", defaultRequestLogPruneInterval),
	})
}

func normalizeConfig(cfg Config) Config {
	if cfg.PublicAddr == "" {
		cfg.PublicAddr = defaultPublicAddr
	}
	if cfg.AgentAddr == "" {
		cfg.AgentAddr = defaultAgentAddr
	}
	if cfg.ManagementAddr == "" {
		cfg.ManagementAddr = defaultManagementAddr
	}
	if cfg.RootDomain == "" {
		cfg.RootDomain = defaultRootDomain
	}
	if cfg.PublicURL == "" {
		cfg.PublicURL = defaultPublicURL
	}
	if cfg.StaticToken == "" {
		cfg.StaticToken = defaultStaticToken
	}
	if cfg.ControlPlaneTimeout <= 0 {
		cfg.ControlPlaneTimeout = defaultControlPlaneTimeout
	}
	if cfg.CustomDomainCacheTTL < 0 {
		cfg.CustomDomainCacheTTL = defaultCustomDomainCacheTTL
	}
	if cfg.CustomDomainMissTTL < 0 {
		cfg.CustomDomainMissTTL = defaultCustomDomainMissTTL
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.MaxConcurrentStreams <= 0 {
		cfg.MaxConcurrentStreams = defaultMaxConcurrentStreams
	}
	if cfg.RateLimitRequestsPerSecond <= 0 {
		cfg.RateLimitRequestsPerSecond = defaultRateLimitRPS
	}
	if cfg.RateLimitBurst <= 0 {
		cfg.RateLimitBurst = defaultRateLimitBurst
	}
	if cfg.StreamChunkBytes <= 0 {
		cfg.StreamChunkBytes = defaultStreamChunkBytes
	}
	if cfg.ReadHeaderTimeout <= 0 {
		cfg.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = defaultReadTimeout
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = defaultHandshakeTimeout
	}
	if cfg.StreamTimeout <= 0 {
		cfg.StreamTimeout = defaultStreamTimeout
	}
	if cfg.WebSocketWriteTimeout <= 0 {
		cfg.WebSocketWriteTimeout = defaultWebSocketWriteTimeout
	}
	if cfg.WebSocketPingInterval <= 0 {
		cfg.WebSocketPingInterval = defaultWebSocketPingInterval
	}
	if cfg.WebSocketPongTimeout <= 0 {
		cfg.WebSocketPongTimeout = defaultWebSocketPongTimeout
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.RequestLogLimit < 0 {
		cfg.RequestLogLimit = defaultRequestLogLimit
	}
	if cfg.RequestLogWriteTimeout <= 0 {
		cfg.RequestLogWriteTimeout = defaultRequestLogWriteTimeout
	}
	if cfg.RequestLogRetention < 0 {
		cfg.RequestLogRetention = defaultRequestLogRetention
	}
	if cfg.RequestLogPruneInterval < 0 {
		cfg.RequestLogPruneInterval = defaultRequestLogPruneInterval
	}
	return cfg
}

func envString(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envInt64(name string, fallback int64) int64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
