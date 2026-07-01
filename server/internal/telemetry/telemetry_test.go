// SPDX-License-Identifier: AGPL-3.0-only

package telemetry

import "testing"

func TestConfigFromEnvDisabledByDefault(t *testing.T) {
	cfg := ConfigFromEnv("porthook-test", "dev")

	if cfg.Enabled {
		t.Fatalf("Enabled = true, want false")
	}
	if cfg.Exporter != "none" {
		t.Fatalf("Exporter = %q, want none", cfg.Exporter)
	}
	if cfg.ServiceName != "porthook-test" {
		t.Fatalf("ServiceName = %q, want porthook-test", cfg.ServiceName)
	}
}

func TestConfigFromEnvEnablesDefaultOTLPExporter(t *testing.T) {
	t.Setenv("PORTHOOK_OTEL_ENABLED", "true")

	cfg := ConfigFromEnv("porthook-test", "dev")

	if !cfg.Enabled {
		t.Fatalf("Enabled = false, want true")
	}
	if cfg.Exporter != "otlp" {
		t.Fatalf("Exporter = %q, want otlp", cfg.Exporter)
	}
	if cfg.Protocol != "http/protobuf" {
		t.Fatalf("Protocol = %q, want http/protobuf", cfg.Protocol)
	}
}

func TestConfigFromEnvHonorsStandardTraceExporter(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "standard-service")
	t.Setenv("OTEL_TRACES_EXPORTER", "stdout")

	cfg := ConfigFromEnv("porthook-test", "dev")

	if !cfg.Enabled {
		t.Fatalf("Enabled = false, want true")
	}
	if cfg.ServiceName != "standard-service" {
		t.Fatalf("ServiceName = %q, want standard-service", cfg.ServiceName)
	}
	if cfg.Exporter != "stdout" {
		t.Fatalf("Exporter = %q, want stdout", cfg.Exporter)
	}
}

func TestConfigFromEnvHonorsSDKDisabled(t *testing.T) {
	t.Setenv("PORTHOOK_OTEL_ENABLED", "true")
	t.Setenv("OTEL_TRACES_EXPORTER", "stdout")
	t.Setenv("OTEL_SDK_DISABLED", "true")

	cfg := ConfigFromEnv("porthook-test", "dev")

	if cfg.Enabled {
		t.Fatalf("Enabled = true, want false")
	}
	if cfg.Exporter != "none" {
		t.Fatalf("Exporter = %q, want none", cfg.Exporter)
	}
}

func TestConfigFromEnvHonorsExplicitDisabledFlag(t *testing.T) {
	t.Setenv("PORTHOOK_OTEL_ENABLED", "false")
	t.Setenv("PORTHOOK_OTEL_EXPORTER", "otlp")

	cfg := ConfigFromEnv("porthook-test", "dev")

	if cfg.Enabled {
		t.Fatalf("Enabled = true, want false")
	}
	if cfg.Exporter != "otlp" {
		t.Fatalf("Exporter = %q, want otlp", cfg.Exporter)
	}
}
