// SPDX-License-Identifier: AGPL-3.0-only

package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/voiteco/porthook/server/internal/requestid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	Enabled        bool
	ServiceName    string
	ServiceVersion string
	Exporter       string
	Protocol       string
}

func ConfigFromEnv(defaultServiceName, serviceVersion string) Config {
	serviceName := firstEnv("PORTHOOK_OTEL_SERVICE_NAME", "OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	exporter := strings.ToLower(firstEnv("PORTHOOK_OTEL_EXPORTER", "OTEL_TRACES_EXPORTER"))
	protocol := strings.ToLower(firstEnv("PORTHOOK_OTEL_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL"))
	enabled, enabledSet := envBool("PORTHOOK_OTEL_ENABLED")
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		enabled = false
		enabledSet = true
		exporter = "none"
	}
	if !enabledSet && exporter != "" && exporter != "none" {
		enabled = true
	}
	if enabled && exporter == "" {
		exporter = "otlp"
	}
	if exporter == "" {
		exporter = "none"
	}
	if protocol == "" {
		protocol = "http/protobuf"
	}
	return Config{
		Enabled:        enabled,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Exporter:       exporter,
		Protocol:       protocol,
	}
}

func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled || cfg.Exporter == "none" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
	))
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return provider.Shutdown, nil
}

func HTTPHandler(handler http.Handler, operation string) http.Handler {
	return otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := requestid.FromRequest(r); id != "" {
			trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("porthook.request_id", id))
		}
		handler.ServeHTTP(w, r)
	}), operation)
}

func HTTPTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base)
}

func SetSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

func RecordSpan(ctx context.Context, status int, outcome string, err error, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	attrs = append(attrs,
		attribute.Int("http.response.status_code", status),
		attribute.String("porthook.outcome", outcome),
	)
	if id := requestid.FromContext(ctx); id != "" {
		attrs = append(attrs, attribute.String("porthook.request_id", id))
	}
	span.SetAttributes(attrs...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	if status >= 500 {
		span.SetStatus(codes.Error, outcome)
	}
}

func newExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch cfg.Exporter {
	case "stdout", "console":
		exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("create stdout trace exporter: %w", err)
		}
		return exporter, nil
	case "otlp":
		return newOTLPExporter(ctx, cfg.Protocol)
	case "otlp-http", "http", "http/protobuf":
		return newOTLPHTTPExporter(ctx)
	case "otlp-grpc", "grpc":
		return newOTLPGRPCExporter(ctx)
	default:
		return nil, fmt.Errorf("unsupported telemetry exporter %q", cfg.Exporter)
	}
}

func newOTLPExporter(ctx context.Context, protocol string) (sdktrace.SpanExporter, error) {
	switch protocol {
	case "", "http", "http/protobuf":
		return newOTLPHTTPExporter(ctx)
	case "grpc":
		return newOTLPGRPCExporter(ctx)
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol %q", protocol)
	}
}

func newOTLPHTTPExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP HTTP trace exporter: %w", err)
	}
	return exporter, nil
}

func newOTLPGRPCExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP gRPC trace exporter: %w", err)
	}
	return exporter, nil
}

func firstEnv(names ...string) string {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			return value
		}
	}
	return ""
}

func envBool(name string) (bool, bool) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return false, false
	}
	return value == "1" || value == "t" || value == "true" || value == "yes" || value == "on", true
}
