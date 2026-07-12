// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultAddr                     = ":8082"
	defaultGatewayManagementTimeout = 5 * time.Second
	defaultAuditEventRetention      = 90 * 24 * time.Hour
	defaultAuditEventPruneInterval  = time.Hour
	defaultShutdownTimeout          = 5 * time.Second
	defaultDBMaxOpenConns           = 20
	defaultDBMaxIdleConns           = 5
	defaultDBConnMaxLifetime        = 30 * time.Minute
	defaultDBConnMaxIdleTime        = 5 * time.Minute
)

type Config struct {
	Addr                     string
	AdminToken               string
	ValidatorToken           string
	GatewayManagementURL     string
	GatewayManagementToken   string
	GatewayManagementTimeout time.Duration
	TrustedProxies           string
	DatabaseURL              string
	Version                  string
	AuditEventRetention      time.Duration
	AuditEventPruneInterval  time.Duration
	// DNSResolverAddr optionally points custom-domain TXT verification
	// lookups at a specific "host:port" DNS server instead of the system
	// resolver. Useful for split-horizon DNS, a private authoritative
	// zone, or local testing. Empty uses the system resolver.
	DNSResolverAddr   string
	ShutdownTimeout   time.Duration
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnMaxIdleTime time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		Addr:                     envString("PORTHOOK_CONTROL_ADDR", defaultAddr),
		AdminToken:               os.Getenv("PORTHOOK_CONTROL_ADMIN_TOKEN"),
		ValidatorToken:           os.Getenv("PORTHOOK_CONTROL_VALIDATOR_TOKEN"),
		GatewayManagementURL:     os.Getenv("PORTHOOK_GATEWAY_MANAGEMENT_URL"),
		GatewayManagementToken:   os.Getenv("PORTHOOK_GATEWAY_MANAGEMENT_TOKEN"),
		GatewayManagementTimeout: envDuration("PORTHOOK_GATEWAY_MANAGEMENT_TIMEOUT", defaultGatewayManagementTimeout),
		TrustedProxies:           os.Getenv("PORTHOOK_TRUSTED_PROXIES"),
		DatabaseURL:              os.Getenv("PORTHOOK_DATABASE_URL"),
		AuditEventRetention:      envDuration("PORTHOOK_AUDIT_EVENT_RETENTION", defaultAuditEventRetention),
		AuditEventPruneInterval:  envDuration("PORTHOOK_AUDIT_EVENT_PRUNE_INTERVAL", defaultAuditEventPruneInterval),
		DNSResolverAddr:          os.Getenv("PORTHOOK_DNS_RESOLVER_ADDR"),
		ShutdownTimeout:          envDuration("PORTHOOK_SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		DBMaxOpenConns:           envInt("PORTHOOK_DB_MAX_OPEN_CONNS", defaultDBMaxOpenConns),
		DBMaxIdleConns:           envInt("PORTHOOK_DB_MAX_IDLE_CONNS", defaultDBMaxIdleConns),
		DBConnMaxLifetime:        envDuration("PORTHOOK_DB_CONN_MAX_LIFETIME", defaultDBConnMaxLifetime),
		DBConnMaxIdleTime:        envDuration("PORTHOOK_DB_CONN_MAX_IDLE_TIME", defaultDBConnMaxIdleTime),
	}
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.DBMaxOpenConns <= 0 {
		cfg.DBMaxOpenConns = defaultDBMaxOpenConns
	}
	if cfg.DBMaxIdleConns < 0 {
		cfg.DBMaxIdleConns = defaultDBMaxIdleConns
	}
	if cfg.DBConnMaxLifetime < 0 {
		cfg.DBConnMaxLifetime = defaultDBConnMaxLifetime
	}
	if cfg.DBConnMaxIdleTime < 0 {
		cfg.DBConnMaxIdleTime = defaultDBConnMaxIdleTime
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
