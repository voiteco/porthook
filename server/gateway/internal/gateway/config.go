// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultPublicAddr            = ":8080"
	defaultAgentAddr             = ":8081"
	defaultRootDomain            = "localhost"
	defaultPublicURL             = "http://localhost:8080"
	defaultStaticToken           = "dev-token"
	defaultMaxBodyBytes          = 1 << 20
	defaultReadHeaderTimeout     = 5 * time.Second
	defaultReadTimeout           = 30 * time.Second
	defaultWriteTimeout          = 35 * time.Second
	defaultIdleTimeout           = 120 * time.Second
	defaultHandshakeTimeout      = 10 * time.Second
	defaultStreamTimeout         = 30 * time.Second
	defaultWebSocketWriteTimeout = 10 * time.Second
	defaultWebSocketPingInterval = 15 * time.Second
	defaultWebSocketPongTimeout  = 5 * time.Second
	defaultShutdownTimeout       = 5 * time.Second
)

type Config struct {
	PublicAddr            string
	AgentAddr             string
	RootDomain            string
	PublicURL             string
	StaticToken           string
	MaxBodyBytes          int64
	ReadHeaderTimeout     time.Duration
	ReadTimeout           time.Duration
	WriteTimeout          time.Duration
	IdleTimeout           time.Duration
	HandshakeTimeout      time.Duration
	StreamTimeout         time.Duration
	WebSocketWriteTimeout time.Duration
	WebSocketPingInterval time.Duration
	WebSocketPongTimeout  time.Duration
	ShutdownTimeout       time.Duration
}

func ConfigFromEnv() Config {
	return normalizeConfig(Config{
		PublicAddr:            envString("PORTHOOK_ADDR", defaultPublicAddr),
		AgentAddr:             envString("PORTHOOK_AGENT_ADDR", defaultAgentAddr),
		RootDomain:            envString("PORTHOOK_ROOT_DOMAIN", defaultRootDomain),
		PublicURL:             envString("PORTHOOK_PUBLIC_URL", defaultPublicURL),
		StaticToken:           envString("PORTHOOK_STATIC_TOKEN", defaultStaticToken),
		MaxBodyBytes:          envInt64("PORTHOOK_MAX_BODY_BYTES", defaultMaxBodyBytes),
		ReadHeaderTimeout:     envDuration("PORTHOOK_READ_HEADER_TIMEOUT", defaultReadHeaderTimeout),
		ReadTimeout:           envDuration("PORTHOOK_READ_TIMEOUT", defaultReadTimeout),
		WriteTimeout:          envDuration("PORTHOOK_WRITE_TIMEOUT", defaultWriteTimeout),
		IdleTimeout:           envDuration("PORTHOOK_IDLE_TIMEOUT", defaultIdleTimeout),
		HandshakeTimeout:      envDuration("PORTHOOK_HANDSHAKE_TIMEOUT", defaultHandshakeTimeout),
		StreamTimeout:         envDuration("PORTHOOK_STREAM_TIMEOUT", defaultStreamTimeout),
		WebSocketWriteTimeout: envDuration("PORTHOOK_WS_WRITE_TIMEOUT", defaultWebSocketWriteTimeout),
		WebSocketPingInterval: envDuration("PORTHOOK_WS_PING_INTERVAL", defaultWebSocketPingInterval),
		WebSocketPongTimeout:  envDuration("PORTHOOK_WS_PONG_TIMEOUT", defaultWebSocketPongTimeout),
		ShutdownTimeout:       envDuration("PORTHOOK_SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
	})
}

func normalizeConfig(cfg Config) Config {
	if cfg.PublicAddr == "" {
		cfg.PublicAddr = defaultPublicAddr
	}
	if cfg.AgentAddr == "" {
		cfg.AgentAddr = defaultAgentAddr
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
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
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

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
