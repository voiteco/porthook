// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultServerURL             = "http://localhost:8081"
	defaultToken                 = "dev-token"
	defaultHandshakeTimeout      = 10 * time.Second
	defaultRequestTimeout        = 30 * time.Second
	defaultMaxResponseBodyBytes  = 1 << 20
	defaultStreamChunkBytes      = 32 << 10
	defaultWebSocketWriteTimeout = 10 * time.Second
	defaultWebSocketPingInterval = 15 * time.Second
	defaultWebSocketPongTimeout  = 5 * time.Second
	defaultReconnectInitialDelay = 500 * time.Millisecond
	defaultReconnectMaxDelay     = 5 * time.Second
	defaultReconnectJitter       = 250 * time.Millisecond
)

type Config struct {
	ServerURL             string
	Token                 string
	RequestedSubdomain    string
	Port                  int
	LocalTarget           string
	AgentVersion          string
	HandshakeTimeout      time.Duration
	RequestTimeout        time.Duration
	MaxResponseBodyBytes  int64
	StreamChunkBytes      int
	WebSocketWriteTimeout time.Duration
	WebSocketPingInterval time.Duration
	WebSocketPongTimeout  time.Duration
	ReconnectInitialDelay time.Duration
	ReconnectMaxDelay     time.Duration
	ReconnectJitter       time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		ServerURL:             defaultServerURL,
		Token:                 defaultToken,
		HandshakeTimeout:      envDuration("PORTHOOK_HANDSHAKE_TIMEOUT", defaultHandshakeTimeout),
		RequestTimeout:        envDuration("PORTHOOK_REQUEST_TIMEOUT", defaultRequestTimeout),
		MaxResponseBodyBytes:  envInt64("PORTHOOK_MAX_RESPONSE_BODY_BYTES", defaultMaxResponseBodyBytes),
		StreamChunkBytes:      envInt("PORTHOOK_STREAM_CHUNK_BYTES", defaultStreamChunkBytes),
		WebSocketWriteTimeout: envDuration("PORTHOOK_WS_WRITE_TIMEOUT", defaultWebSocketWriteTimeout),
		WebSocketPingInterval: envDuration("PORTHOOK_WS_PING_INTERVAL", defaultWebSocketPingInterval),
		WebSocketPongTimeout:  envDuration("PORTHOOK_WS_PONG_TIMEOUT", defaultWebSocketPongTimeout),
		ReconnectInitialDelay: envDuration("PORTHOOK_RECONNECT_INITIAL_DELAY", defaultReconnectInitialDelay),
		ReconnectMaxDelay:     envDuration("PORTHOOK_RECONNECT_MAX_DELAY", defaultReconnectMaxDelay),
		ReconnectJitter:       envDuration("PORTHOOK_RECONNECT_JITTER", defaultReconnectJitter),
	}

	if fileCfg, ok, err := LoadConfigFile(); err == nil && ok {
		if fileCfg.ServerURL != "" {
			cfg.ServerURL = fileCfg.ServerURL
		}
		if fileCfg.Token != "" {
			cfg.Token = fileCfg.Token
		}
	}

	cfg.ServerURL = envString("PORTHOOK_SERVER_URL", cfg.ServerURL)
	cfg.Token = envString("PORTHOOK_TOKEN", cfg.Token)

	return normalizeConfig(cfg)
}

func normalizeConfig(cfg Config) Config {
	if cfg.ServerURL == "" {
		cfg.ServerURL = defaultServerURL
	}
	if cfg.Token == "" {
		cfg.Token = defaultToken
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = defaultHandshakeTimeout
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		cfg.MaxResponseBodyBytes = defaultMaxResponseBodyBytes
	}
	if cfg.StreamChunkBytes <= 0 {
		cfg.StreamChunkBytes = defaultStreamChunkBytes
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
	if cfg.ReconnectInitialDelay <= 0 {
		cfg.ReconnectInitialDelay = defaultReconnectInitialDelay
	}
	if cfg.ReconnectMaxDelay <= 0 {
		cfg.ReconnectMaxDelay = defaultReconnectMaxDelay
	}
	if cfg.ReconnectJitter < 0 {
		cfg.ReconnectJitter = defaultReconnectJitter
	}
	if cfg.ReconnectMaxDelay < cfg.ReconnectInitialDelay {
		cfg.ReconnectMaxDelay = cfg.ReconnectInitialDelay
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
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
