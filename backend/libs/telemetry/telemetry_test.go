package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/bdsplatform/platform/backend/libs/config"
)

func TestInitWithoutExporter(t *testing.T) {
	cfg := config.Config{
		ServiceName: "test",
		Environment: config.EnvDevelopment,
		OTEL:        config.OTELConfig{Endpoint: "", SampleRatio: 1.0},
	}

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	// Propagators must be installed even without an exporter.
	if _, ok := otel.GetTextMapPropagator().(propagation.TextMapPropagator); !ok {
		t.Fatal("expected a text map propagator to be configured")
	}

	// A span can be created and carries a valid context.
	_, span := Tracer("test").Start(context.Background(), "op")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("expected a valid span context")
	}
}

func TestShutdownIsSafe(t *testing.T) {
	shutdown, err := Init(context.Background(), config.Config{ServiceName: "x", Environment: config.EnvDevelopment})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
