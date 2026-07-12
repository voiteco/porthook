// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"testing"
	"time"
)

func TestConfigFromEnvSupportsAuditPruningSettings(t *testing.T) {
	t.Setenv("PORTHOOK_AUDIT_EVENT_RETENTION", "0s")
	t.Setenv("PORTHOOK_AUDIT_EVENT_PRUNE_INTERVAL", "15m")

	cfg := ConfigFromEnv()
	if cfg.AuditEventRetention != 0 {
		t.Fatalf("AuditEventRetention = %s, want 0s", cfg.AuditEventRetention)
	}
	if cfg.AuditEventPruneInterval != 15*time.Minute {
		t.Fatalf("AuditEventPruneInterval = %s, want 15m", cfg.AuditEventPruneInterval)
	}
}

func TestConfigFromEnvSupportsGatewayManagement(t *testing.T) {
	t.Setenv("PORTHOOK_GATEWAY_MANAGEMENT_URL", "http://gateway:8082")
	t.Setenv("PORTHOOK_GATEWAY_MANAGEMENT_TOKEN", "management-secret")
	t.Setenv("PORTHOOK_GATEWAY_MANAGEMENT_TIMEOUT", "3s")

	cfg := ConfigFromEnv()
	if cfg.GatewayManagementURL != "http://gateway:8082" {
		t.Fatalf("GatewayManagementURL = %q", cfg.GatewayManagementURL)
	}
	if cfg.GatewayManagementToken != "management-secret" {
		t.Fatalf("GatewayManagementToken = %q", cfg.GatewayManagementToken)
	}
	if cfg.GatewayManagementTimeout != 3*time.Second {
		t.Fatalf("GatewayManagementTimeout = %s, want 3s", cfg.GatewayManagementTimeout)
	}
}

func TestConfigFromEnvSupportsTrustedProxies(t *testing.T) {
	t.Setenv("PORTHOOK_TRUSTED_PROXIES", "10.0.0.0/8,2001:db8::/32")

	if got := ConfigFromEnv().TrustedProxies; got != "10.0.0.0/8,2001:db8::/32" {
		t.Fatalf("TrustedProxies = %q", got)
	}
}

func TestValidateConfigRejectsInvalidTrustedProxies(t *testing.T) {
	cfg := Config{TrustedProxies: "invalid"}

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_TRUSTED_PROXIES" {
			return
		}
	}
	t.Fatalf("errors = %+v, want PORTHOOK_TRUSTED_PROXIES error", report.Errors)
}

func TestValidateConfigRequiresTrustedProxiesInProduction(t *testing.T) {
	cfg := Config{}

	report := ValidateConfig(cfg, ConfigValidationOptions{Production: true})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_TRUSTED_PROXIES" {
			return
		}
	}
	t.Fatalf("errors = %+v, want PORTHOOK_TRUSTED_PROXIES error", report.Errors)
}

func TestValidateConfigRequiresGatewayManagementToken(t *testing.T) {
	cfg := Config{
		GatewayManagementURL:     "http://gateway:8082",
		GatewayManagementTimeout: time.Second,
	}

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_GATEWAY_MANAGEMENT_TOKEN" {
			return
		}
	}
	t.Fatalf("errors = %+v, want gateway management token error", report.Errors)
}

func TestConfigFromEnvSupportsDNSResolverAddr(t *testing.T) {
	t.Setenv("PORTHOOK_DNS_RESOLVER_ADDR", "127.0.0.1:15353")

	if got := ConfigFromEnv().DNSResolverAddr; got != "127.0.0.1:15353" {
		t.Fatalf("DNSResolverAddr = %q, want 127.0.0.1:15353", got)
	}
}

func TestValidateConfigAllowsEmptyDNSResolverAddr(t *testing.T) {
	cfg := Config{}

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_DNS_RESOLVER_ADDR" {
			t.Fatalf("errors = %+v, want no PORTHOOK_DNS_RESOLVER_ADDR error", report.Errors)
		}
	}
}

func TestValidateConfigRejectsInvalidDNSResolverAddr(t *testing.T) {
	cfg := Config{DNSResolverAddr: "not-a-host-port"}

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_DNS_RESOLVER_ADDR" {
			return
		}
	}
	t.Fatalf("errors = %+v, want PORTHOOK_DNS_RESOLVER_ADDR error", report.Errors)
}

func TestConfigFromEnvSupportsShutdownTimeout(t *testing.T) {
	t.Setenv("PORTHOOK_SHUTDOWN_TIMEOUT", "9s")

	if got := ConfigFromEnv().ShutdownTimeout; got != 9*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 9s", got)
	}
}

func TestConfigFromEnvDefaultsShutdownTimeout(t *testing.T) {
	if got := ConfigFromEnv().ShutdownTimeout; got != defaultShutdownTimeout {
		t.Fatalf("ShutdownTimeout = %s, want default %s", got, defaultShutdownTimeout)
	}
}

func TestConfigFromEnvSupportsDBPoolLimits(t *testing.T) {
	t.Setenv("PORTHOOK_DB_MAX_OPEN_CONNS", "10")
	t.Setenv("PORTHOOK_DB_MAX_IDLE_CONNS", "3")
	t.Setenv("PORTHOOK_DB_CONN_MAX_LIFETIME", "15m")
	t.Setenv("PORTHOOK_DB_CONN_MAX_IDLE_TIME", "2m")

	cfg := ConfigFromEnv()
	if cfg.DBMaxOpenConns != 10 {
		t.Fatalf("DBMaxOpenConns = %d, want 10", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 3 {
		t.Fatalf("DBMaxIdleConns = %d, want 3", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 15*time.Minute {
		t.Fatalf("DBConnMaxLifetime = %s, want 15m", cfg.DBConnMaxLifetime)
	}
	if cfg.DBConnMaxIdleTime != 2*time.Minute {
		t.Fatalf("DBConnMaxIdleTime = %s, want 2m", cfg.DBConnMaxIdleTime)
	}
}

func TestConfigFromEnvDefaultsDBPoolLimits(t *testing.T) {
	cfg := ConfigFromEnv()
	if cfg.DBMaxOpenConns != defaultDBMaxOpenConns {
		t.Fatalf("DBMaxOpenConns = %d, want default %d", cfg.DBMaxOpenConns, defaultDBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != defaultDBMaxIdleConns {
		t.Fatalf("DBMaxIdleConns = %d, want default %d", cfg.DBMaxIdleConns, defaultDBMaxIdleConns)
	}
}

func TestValidateConfigWarnsWhenDBMaxIdleConnsExceedsMaxOpenConns(t *testing.T) {
	cfg := Config{DBMaxOpenConns: 5, DBMaxIdleConns: 50, ShutdownTimeout: time.Second}

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Warnings {
		if issue.Field == "PORTHOOK_DB_MAX_IDLE_CONNS" {
			return
		}
	}
	t.Fatalf("warnings = %+v, want PORTHOOK_DB_MAX_IDLE_CONNS warning", report.Warnings)
}
