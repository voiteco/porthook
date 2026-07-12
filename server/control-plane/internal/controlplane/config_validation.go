// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/voiteco/porthook/server/internal/clientip"
)

type ConfigValidationOptions struct {
	Production bool
}

type ConfigValidationProblem struct {
	Field   string
	Message string
}

type ConfigValidationReport struct {
	Errors   []ConfigValidationProblem
	Warnings []ConfigValidationProblem
}

func (r ConfigValidationReport) HasErrors() bool {
	return len(r.Errors) > 0
}

func ValidateConfig(cfg Config, opts ConfigValidationOptions) ConfigValidationReport {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	var report ConfigValidationReport

	addError := func(field, message string) {
		report.Errors = append(report.Errors, ConfigValidationProblem{Field: field, Message: message})
	}
	addWarning := func(field, message string) {
		report.Warnings = append(report.Warnings, ConfigValidationProblem{Field: field, Message: message})
	}

	if err := validateListenAddr(cfg.Addr); err != nil {
		addError("PORTHOOK_CONTROL_ADDR", err.Error())
	}
	if _, err := clientip.ParseTrustedProxies(cfg.TrustedProxies); err != nil {
		addError("PORTHOOK_TRUSTED_PROXIES", err.Error())
	} else if opts.Production && strings.TrimSpace(cfg.TrustedProxies) == "" {
		addError("PORTHOOK_TRUSTED_PROXIES", "is required in production mode")
	}
	if strings.TrimSpace(cfg.AdminToken) == "" {
		if opts.Production {
			addError("PORTHOOK_CONTROL_ADMIN_TOKEN", "is required in production mode")
		} else {
			addWarning("PORTHOOK_CONTROL_ADMIN_TOKEN", "is not configured; admin token APIs will be disabled")
		}
	} else if opts.Production && looksLikePlaceholderSecret(cfg.AdminToken) {
		addError("PORTHOOK_CONTROL_ADMIN_TOKEN", "must be replaced with a generated secret in production mode")
	}
	if opts.Production && strings.TrimSpace(cfg.AdminToken) != "" {
		addWarning("PORTHOOK_CONTROL_ADMIN_TOKEN", "is a full-scope bootstrap/recovery token; create scoped admin tokens for routine operations and keep this value offline")
	}
	if strings.TrimSpace(cfg.ValidatorToken) == "" {
		if opts.Production {
			addError("PORTHOOK_CONTROL_VALIDATOR_TOKEN", "is required in production mode")
		} else {
			addWarning("PORTHOOK_CONTROL_VALIDATOR_TOKEN", "is not configured; gateway token validation will be disabled")
		}
	} else if opts.Production && looksLikePlaceholderSecret(cfg.ValidatorToken) {
		addError("PORTHOOK_CONTROL_VALIDATOR_TOKEN", "must be replaced with a generated secret in production mode")
	}
	if cfg.AdminToken != "" && cfg.AdminToken == cfg.ValidatorToken {
		if opts.Production {
			addError("PORTHOOK_CONTROL_VALIDATOR_TOKEN", "must be different from PORTHOOK_CONTROL_ADMIN_TOKEN in production mode")
		} else {
			addWarning("PORTHOOK_CONTROL_VALIDATOR_TOKEN", "matches PORTHOOK_CONTROL_ADMIN_TOKEN")
		}
	}
	gatewayManagementConfigured := strings.TrimSpace(cfg.GatewayManagementURL) != ""
	if !gatewayManagementConfigured {
		if opts.Production {
			addError("PORTHOOK_GATEWAY_MANAGEMENT_URL", "is required in production mode")
		} else {
			addWarning("PORTHOOK_GATEWAY_MANAGEMENT_URL", "is not configured; gateway operator APIs will be disabled")
		}
	} else if err := validateHTTPURL(cfg.GatewayManagementURL); err != nil {
		addError("PORTHOOK_GATEWAY_MANAGEMENT_URL", err.Error())
	}
	gatewayManagementTokenConfigured := strings.TrimSpace(cfg.GatewayManagementToken) != ""
	if !gatewayManagementTokenConfigured {
		if gatewayManagementConfigured || opts.Production {
			addError("PORTHOOK_GATEWAY_MANAGEMENT_TOKEN", "is required when gateway management is configured")
		}
	} else if opts.Production && looksLikePlaceholderSecret(cfg.GatewayManagementToken) {
		addError("PORTHOOK_GATEWAY_MANAGEMENT_TOKEN", "must be replaced with a generated secret in production mode")
	}
	if opts.Production && gatewayManagementTokenConfigured && (cfg.GatewayManagementToken == cfg.AdminToken || cfg.GatewayManagementToken == cfg.ValidatorToken) {
		addError("PORTHOOK_GATEWAY_MANAGEMENT_TOKEN", "must be different from control-plane admin and validator tokens in production mode")
	}
	if cfg.GatewayManagementTimeout <= 0 {
		addError("PORTHOOK_GATEWAY_MANAGEMENT_TIMEOUT", "must be positive")
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		if opts.Production {
			addError("PORTHOOK_DATABASE_URL", "is required in production mode")
		} else {
			addWarning("PORTHOOK_DATABASE_URL", "is not configured; control-plane data will be in-memory only")
		}
	}
	if cfg.AuditEventRetention < 0 {
		addError("PORTHOOK_AUDIT_EVENT_RETENTION", "must not be negative")
	}
	if cfg.AuditEventPruneInterval < 0 {
		addError("PORTHOOK_AUDIT_EVENT_PRUNE_INTERVAL", "must not be negative")
	}
	if resolverAddr := strings.TrimSpace(cfg.DNSResolverAddr); resolverAddr != "" {
		if err := validateHostPortAddr(resolverAddr); err != nil {
			addError("PORTHOOK_DNS_RESOLVER_ADDR", err.Error())
		}
	}

	return report
}

func validateHTTPURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("must be a valid URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("must use http or https and include a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("must not include userinfo, query, or fragment")
	}
	return nil
}

func validateListenAddr(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("must not be empty")
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("must be a listen address such as :8082 or 127.0.0.1:8082")
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed <= 0 || parsed > 65535 {
		return fmt.Errorf("must include a TCP port between 1 and 65535")
	}
	return nil
}

func validateHostPortAddr(value string) error {
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("must be a \"host:port\" address")
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed <= 0 || parsed > 65535 {
		return fmt.Errorf("must include a TCP port between 1 and 65535")
	}
	return nil
}

func looksLikePlaceholderSecret(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" ||
		strings.Contains(value, "change-me") ||
		strings.Contains(value, "changeme") ||
		strings.Contains(value, "dev-token")
}
