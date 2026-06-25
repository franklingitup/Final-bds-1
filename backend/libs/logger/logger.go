// Package logger provides structured logging built on log/slog.
//
// Loggers emitted through this package automatically enrich every record with
// the correlation ID and the active OpenTelemetry trace/span IDs found in the
// context, giving fully correlated logs across services.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/bdsplatform/platform/backend/libs/config"
)

// New builds a logger from configuration, installs it as the slog default, and
// returns it. Output is written to stdout.
func New(cfg config.Config) *slog.Logger {
	return NewWithWriter(cfg, os.Stdout)
}

// NewWithWriter is like New but writes to the supplied writer (useful in tests).
func NewWithWriter(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.Log.Level)}

	var base slog.Handler
	switch cfg.Log.Format {
	case "text":
		base = slog.NewTextHandler(w, opts)
	default:
		base = slog.NewJSONHandler(w, opts)
	}

	handler := &contextHandler{Handler: base}
	logger := slog.New(handler).With(
		slog.String("service", cfg.ServiceName),
		slog.String("env", string(cfg.Environment)),
	)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// contextHandler enriches records with correlation and trace context.
type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlation_id", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}
