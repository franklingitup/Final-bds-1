// Package telemetry wires OpenTelemetry distributed tracing and Prometheus
// metrics for every service.
//
// Init configures a global TracerProvider with an OTLP/gRPC exporter (when an
// endpoint is configured), a parent-based ratio sampler, a service resource, and
// the W3C trace-context + baggage propagators. It returns a shutdown function
// that flushes and stops the exporter for graceful termination.
package telemetry

import (
	"context"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// ShutdownFunc flushes and stops telemetry exporters.
type ShutdownFunc func(context.Context) error

// Init configures global tracing. The returned ShutdownFunc must be called on
// service shutdown. It is always non-nil, even on error or when export is
// disabled, so callers can defer it unconditionally.
func Init(ctx context.Context, cfg config.Config) (ShutdownFunc, error) {
	// Propagators are always installed so trace context flows across services
	// regardless of whether spans are exported locally.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("deployment.environment", string(cfg.Environment)),
	))
	if err != nil {
		return noopShutdown, fmt.Errorf("telemetry: build resource: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.OTEL.SampleRatio))

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}

	// Only attach an exporter when an endpoint is configured.
	if cfg.OTEL.Endpoint != "" {
		exp, err := newExporter(ctx, cfg.OTEL)
		if err != nil {
			return noopShutdown, err
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func newExporter(ctx context.Context, cfg config.OTELConfig) (*otlptracegrpc.Exporter, error) {
	endpoint := cfg.Endpoint
	if u, err := url.Parse(cfg.Endpoint); err == nil && u.Host != "" {
		endpoint = u.Host
	}

	grpcOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	if cfg.Insecure {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
	}

	exp, err := otlptracegrpc.New(ctx, grpcOpts...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create OTLP exporter: %w", err)
	}
	return exp, nil
}

// Tracer returns a named tracer from the global provider.
func Tracer(name string) trace.Tracer { return otel.Tracer(name) }

func noopShutdown(context.Context) error { return nil }
