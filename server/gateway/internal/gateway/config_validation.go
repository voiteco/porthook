// SPDX-License-Identifier: AGPL-3.0-only

package gateway

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
	cfg = normalizeConfig(cfg)
	var report ConfigValidationReport

	addError := func(field, message string) {
		report.Errors = append(report.Errors, ConfigValidationProblem{Field: field, Message: message})
	}
	addWarning := func(field, message string) {
		report.Warnings = append(report.Warnings, ConfigValidationProblem{Field: field, Message: message})
	}

	if err := validateListenAddr(cfg.PublicAddr); err != nil {
		addError("PORTHOOK_ADDR", err.Error())
	}
	if err := validateListenAddr(cfg.AgentAddr); err != nil {
		addError("PORTHOOK_AGENT_ADDR", err.Error())
	}
	if err := validateListenAddr(cfg.ManagementAddr); err != nil {
		addError("PORTHOOK_MANAGEMENT_ADDR", err.Error())
	}
	if _, err := clientip.ParseTrustedProxies(cfg.TrustedProxies); err != nil {
		addError("PORTHOOK_TRUSTED_PROXIES", err.Error())
	} else if opts.Production && strings.TrimSpace(cfg.TrustedProxies) == "" {
		addError("PORTHOOK_TRUSTED_PROXIES", "is required in production mode")
	}
	if strings.TrimSpace(cfg.ManagementToken) == "" {
		if opts.Production {
			addError("PORTHOOK_MANAGEMENT_TOKEN", "is required in production mode")
		}
	} else if opts.Production && looksLikePlaceholderSecret(cfg.ManagementToken) {
		addError("PORTHOOK_MANAGEMENT_TOKEN", "must be replaced with a generated secret in production mode")
	}
	rootDomainErr := validateRootDomain(cfg.RootDomain)
	if rootDomainErr != nil {
		addError("PORTHOOK_ROOT_DOMAIN", rootDomainErr.Error())
	} else if opts.Production && strings.EqualFold(cfg.RootDomain, "localhost") {
		addError("PORTHOOK_ROOT_DOMAIN", "must not be localhost in production mode")
	}
	publicURLErr := validateHTTPURL("PORTHOOK_PUBLIC_URL", cfg.PublicURL, opts.Production)
	if publicURLErr != nil {
		addError("PORTHOOK_PUBLIC_URL", publicURLErr.Error())
	}
	if rootDomainErr == nil && publicURLErr == nil {
		if publicHost, ok := hostnameWithoutPort(cfg.PublicURL); ok && !strings.EqualFold(publicHost, cfg.RootDomain) {
			addWarning("PORTHOOK_PUBLIC_URL", fmt.Sprintf("host %q does not match PORTHOOK_ROOT_DOMAIN %q; the gateway announces tunnel URLs on PORTHOOK_ROOT_DOMAIN and only reuses PORTHOOK_PUBLIC_URL's scheme and port, so this host is likely a misconfiguration", publicHost, cfg.RootDomain))
		}
	}

	controlPlaneConfigured := strings.TrimSpace(cfg.ControlPlaneURL) != ""
	if controlPlaneConfigured {
		if err := validateHTTPURL("PORTHOOK_CONTROL_PLANE_URL", cfg.ControlPlaneURL, false); err != nil {
			addError("PORTHOOK_CONTROL_PLANE_URL", err.Error())
		}
		if strings.TrimSpace(cfg.ControlPlaneToken) == "" {
			addError("PORTHOOK_CONTROL_PLANE_TOKEN", "is required when PORTHOOK_CONTROL_PLANE_URL is configured")
		} else if opts.Production && looksLikePlaceholderSecret(cfg.ControlPlaneToken) {
			addError("PORTHOOK_CONTROL_PLANE_TOKEN", "must be replaced with a generated secret in production mode")
		}
	} else {
		if opts.Production {
			addError("PORTHOOK_CONTROL_PLANE_URL", "is required in production mode")
		}
		if strings.TrimSpace(cfg.StaticToken) == "" {
			addError("PORTHOOK_STATIC_TOKEN", "is required when no control plane is configured")
		} else if cfg.StaticToken == defaultStaticToken {
			if opts.Production {
				addError("PORTHOOK_STATIC_TOKEN", "must not use the default development token in production mode")
			} else {
				addWarning("PORTHOOK_STATIC_TOKEN", "uses the default development token")
			}
		}
	}

	if cfg.ControlPlaneTimeout <= 0 {
		addError("PORTHOOK_CONTROL_PLANE_TIMEOUT", "must be positive")
	}
	if cfg.MaxBodyBytes <= 0 {
		addError("PORTHOOK_MAX_BODY_BYTES", "must be positive")
	}
	if cfg.WSMessageMaxBytes <= 0 {
		addError("PORTHOOK_WS_MESSAGE_MAX_BYTES", "must be positive")
	}
	if cfg.MaxConcurrentStreams <= 0 {
		addError("PORTHOOK_MAX_CONCURRENT_STREAMS", "must be positive")
	}
	if cfg.RateLimitRequestsPerSecond <= 0 {
		addError("PORTHOOK_RATE_LIMIT_RPS", "must be positive")
	}
	if cfg.RateLimitBurst <= 0 {
		addError("PORTHOOK_RATE_LIMIT_BURST", "must be positive")
	}
	if cfg.AuthAttemptLimit <= 0 {
		addError("PORTHOOK_AUTH_ATTEMPT_LIMIT", "must be positive")
	}
	if cfg.AuthAttemptWindow <= 0 {
		addError("PORTHOOK_AUTH_ATTEMPT_WINDOW", "must be positive")
	}
	if cfg.StreamChunkBytes <= 0 {
		addError("PORTHOOK_STREAM_CHUNK_BYTES", "must be positive")
	}
	if cfg.ReadHeaderTimeout <= 0 {
		addError("PORTHOOK_READ_HEADER_TIMEOUT", "must be positive")
	}
	if cfg.ReadTimeout <= 0 {
		addError("PORTHOOK_READ_TIMEOUT", "must be positive")
	}
	if cfg.WriteTimeout <= 0 {
		addError("PORTHOOK_WRITE_TIMEOUT", "must be positive")
	}
	if cfg.IdleTimeout <= 0 {
		addError("PORTHOOK_IDLE_TIMEOUT", "must be positive")
	}
	if cfg.HandshakeTimeout <= 0 {
		addError("PORTHOOK_HANDSHAKE_TIMEOUT", "must be positive")
	}
	if cfg.StreamRequestTimeout <= 0 {
		addError("PORTHOOK_STREAM_REQUEST_TIMEOUT", "must be positive")
	}
	if cfg.StreamIdleTimeout < 0 {
		addError("PORTHOOK_STREAM_IDLE_TIMEOUT", "must not be negative")
	}
	if cfg.StreamMaxLifetime <= 0 {
		addError("PORTHOOK_STREAM_MAX_LIFETIME", "must be positive")
	}
	if cfg.WebSocketWriteTimeout <= 0 {
		addError("PORTHOOK_WS_WRITE_TIMEOUT", "must be positive")
	}
	if cfg.WebSocketPingInterval <= 0 {
		addError("PORTHOOK_WS_PING_INTERVAL", "must be positive")
	}
	if cfg.WebSocketPongTimeout <= 0 {
		addError("PORTHOOK_WS_PONG_TIMEOUT", "must be positive")
	}
	if cfg.ShutdownTimeout <= 0 {
		addError("PORTHOOK_SHUTDOWN_TIMEOUT", "must be positive")
	}
	if cfg.RequestLogLimit < 0 {
		addError("PORTHOOK_REQUEST_LOG_LIMIT", "must not be negative")
	}
	if cfg.RequestLogWriteTimeout <= 0 {
		addError("PORTHOOK_REQUEST_LOG_WRITE_TIMEOUT", "must be positive")
	}
	if cfg.RequestLogRetention < 0 {
		addError("PORTHOOK_REQUEST_LOG_RETENTION", "must not be negative")
	}
	if cfg.RequestLogPruneInterval < 0 {
		addError("PORTHOOK_REQUEST_LOG_PRUNE_INTERVAL", "must not be negative")
	}
	if opts.Production && strings.TrimSpace(cfg.RequestLogDatabaseURL) == "" {
		addWarning("PORTHOOK_REQUEST_LOG_DATABASE_URL", "is not configured; gateway request logs will be in-memory only")
	}

	return report
}

func validateListenAddr(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("must not be empty")
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("must be a listen address such as :8080 or 127.0.0.1:8080")
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed <= 0 || parsed > 65535 {
		return fmt.Errorf("must include a TCP port between 1 and 65535")
	}
	return nil
}

func validateRootDomain(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.Contains(value, "://") || strings.ContainsAny(value, "/:") {
		return fmt.Errorf("must be a hostname, not a URL")
	}
	if strings.HasPrefix(value, "*.") {
		return fmt.Errorf("must omit the wildcard prefix")
	}
	return nil
}

func validateHTTPURL(field, value string, requireHTTPS bool) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("must be a valid URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("must include scheme and host")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("must use http or https")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https in production mode", field)
	}
	return nil
}

// hostnameWithoutPort returns rawURL's host with any port stripped, or
// ok=false if rawURL does not parse or has no host.
func hostnameWithoutPort(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return "", false
	}
	if host, _, err := net.SplitHostPort(parsed.Host); err == nil {
		return host, true
	}
	return parsed.Host, true
}

func looksLikePlaceholderSecret(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" ||
		strings.Contains(value, "change-me") ||
		strings.Contains(value, "changeme") ||
		strings.Contains(value, "dev-token")
}
