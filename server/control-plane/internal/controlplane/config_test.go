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
