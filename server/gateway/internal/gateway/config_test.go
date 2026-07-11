// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import "testing"

func TestConfigFromEnvSupportsManagementAddr(t *testing.T) {
	t.Setenv("PORTHOOK_MANAGEMENT_ADDR", "127.0.0.1:19082")
	t.Setenv("PORTHOOK_MANAGEMENT_TOKEN", "management-secret")

	cfg := ConfigFromEnv()
	if cfg.ManagementAddr != "127.0.0.1:19082" {
		t.Fatalf("ManagementAddr = %q, want 127.0.0.1:19082", cfg.ManagementAddr)
	}
	if cfg.ManagementToken != "management-secret" {
		t.Fatalf("ManagementToken = %q, want management-secret", cfg.ManagementToken)
	}
}

func TestValidateConfigRequiresManagementTokenInProduction(t *testing.T) {
	cfg := testConfig()
	cfg.ManagementToken = ""

	report := ValidateConfig(cfg, ConfigValidationOptions{Production: true})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_MANAGEMENT_TOKEN" {
			return
		}
	}
	t.Fatalf("errors = %+v, want PORTHOOK_MANAGEMENT_TOKEN error", report.Errors)
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
