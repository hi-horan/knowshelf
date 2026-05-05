// Package observability 初始化 Knowshelf 的 OpenTelemetry 能力。
package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"knowshelf/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type ShutdownFunc func(context.Context) error

func ConfigureTracing(ctx context.Context, cfg *config.Config) (ShutdownFunc, error) {
	traceCfg := cfg.Observability.Trace
	if !traceCfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}
	sampleRatio := traceCfg.SampleRatio
	if sampleRatio <= 0 || sampleRatio > 1 {
		sampleRatio = 1
	}
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(traceResource(cfg)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	}
	exporter, err := traceExporter(ctx, traceCfg)
	if err != nil {
		return nil, err
	}
	if exporter != nil {
		options = append(options, sdktrace.WithBatcher(exporter))
	}
	provider := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return provider.Shutdown, nil
}

func traceResource(cfg *config.Config) *resource.Resource {
	return resource.NewWithAttributes("",
		attribute.String("service.name", cfg.Observability.ServiceName),
		attribute.String("service.version", cfg.Observability.ServiceVersion),
	)
}

func traceExporter(ctx context.Context, cfg config.TraceConfig) (sdktrace.SpanExporter, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Exporter)) {
	case "", "none":
		return nil, nil
	case "otlp", "otlp_http", "otlp-http":
		options := []otlptracehttp.Option{}
		if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
			if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
				options = append(options, otlptracehttp.WithEndpointURL(endpoint))
			} else {
				options = append(options, otlptracehttp.WithEndpoint(endpoint))
			}
		}
		if cfg.Insecure {
			options = append(options, otlptracehttp.WithInsecure())
		}
		exporter, err := otlptracehttp.New(ctx, options...)
		if err != nil {
			return nil, fmt.Errorf("create otlp trace exporter: %w", err)
		}
		return exporter, nil
	default:
		return nil, errors.New("unsupported trace exporter: " + cfg.Exporter)
	}
}
