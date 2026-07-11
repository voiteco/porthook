// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import "testing"

func TestConfigFromEnvSupportsManagementAddr(t *testing.T) {
	t.Setenv("PORTHOOK_MANAGEMENT_ADDR", "127.0.0.1:19082")

	cfg := ConfigFromEnv()
	if cfg.ManagementAddr != "127.0.0.1:19082" {
		t.Fatalf("ManagementAddr = %q, want 127.0.0.1:19082", cfg.ManagementAddr)
	}
}

func TestValidateConfigRejectsInvalidManagementAddr(t *testing.T) {
	cfg := testConfig()
	cfg.ManagementAddr = "invalid"

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_MANAGEMENT_ADDR" {
			return
		}
	}
	t.Fatalf("errors = %+v, want PORTHOOK_MANAGEMENT_ADDR error", report.Errors)
}
