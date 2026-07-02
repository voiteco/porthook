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
