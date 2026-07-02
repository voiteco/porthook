// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"os"
	"time"
)

const (
	defaultAddr                    = ":8082"
	defaultAuditEventRetention     = 90 * 24 * time.Hour
	defaultAuditEventPruneInterval = time.Hour
)

type Config struct {
	Addr                    string
	AdminToken              string
	ValidatorToken          string
	DatabaseURL             string
	Version                 string
	AuditEventRetention     time.Duration
	AuditEventPruneInterval time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		Addr:                    envString("PORTHOOK_CONTROL_ADDR", defaultAddr),
		AdminToken:              os.Getenv("PORTHOOK_CONTROL_ADMIN_TOKEN"),
		ValidatorToken:          os.Getenv("PORTHOOK_CONTROL_VALIDATOR_TOKEN"),
		DatabaseURL:             os.Getenv("PORTHOOK_DATABASE_URL"),
		AuditEventRetention:     envDuration("PORTHOOK_AUDIT_EVENT_RETENTION", defaultAuditEventRetention),
		AuditEventPruneInterval: envDuration("PORTHOOK_AUDIT_EVENT_PRUNE_INTERVAL", defaultAuditEventPruneInterval),
	}
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
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
