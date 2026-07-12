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

func TestConfigFromEnvSupportsTrustedProxies(t *testing.T) {
	t.Setenv("PORTHOOK_TRUSTED_PROXIES", "10.0.0.0/8,2001:db8::/32")

	if got := ConfigFromEnv().TrustedProxies; got != "10.0.0.0/8,2001:db8::/32" {
		t.Fatalf("TrustedProxies = %q", got)
	}
}

func TestValidateConfigRejectsInvalidTrustedProxies(t *testing.T) {
	cfg := testConfig()
	cfg.TrustedProxies = "invalid"

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_TRUSTED_PROXIES" {
			return
		}
	}
	t.Fatalf("errors = %+v, want PORTHOOK_TRUSTED_PROXIES error", report.Errors)
}

func TestValidateConfigRequiresTrustedProxiesInProduction(t *testing.T) {
	cfg := testConfig()
	cfg.TrustedProxies = ""

	report := ValidateConfig(cfg, ConfigValidationOptions{Production: true})
	for _, issue := range report.Errors {
		if issue.Field == "PORTHOOK_TRUSTED_PROXIES" {
			return
		}
	}
	t.Fatalf("errors = %+v, want PORTHOOK_TRUSTED_PROXIES error", report.Errors)
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

func TestValidateConfigWarnsWhenPublicURLHostDoesNotMatchRootDomain(t *testing.T) {
	cfg := testConfig()
	cfg.RootDomain = "tunnels.example.com"
	cfg.PublicURL = "https://totally-different-host.example"

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Warnings {
		if issue.Field == "PORTHOOK_PUBLIC_URL" {
			return
		}
	}
	t.Fatalf("warnings = %+v, want PORTHOOK_PUBLIC_URL warning", report.Warnings)
}

func TestValidateConfigDoesNotWarnWhenPublicURLHostMatchesRootDomain(t *testing.T) {
	cfg := testConfig()
	cfg.RootDomain = "tunnels.example.com"
	cfg.PublicURL = "https://tunnels.example.com:8443"

	report := ValidateConfig(cfg, ConfigValidationOptions{})
	for _, issue := range report.Warnings {
		if issue.Field == "PORTHOOK_PUBLIC_URL" {
			t.Fatalf("warnings = %+v, want no PORTHOOK_PUBLIC_URL warning", report.Warnings)
		}
	}
}
