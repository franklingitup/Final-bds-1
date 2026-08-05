// Package middleware provides Fiber middleware for correlation IDs, distributed
// tracing, structured request logging, tenant extraction, panic recovery, and
// consistent error responses.
//
// Middleware propagate values through the request's UserContext so downstream
// handlers and the logger automatically see the correlation ID, trace context,
// and tenant organization.
package middleware

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/bdsplatform/platform/backend/libs/authz"
	"github.com/bdsplatform/platform/backend/libs/errors"
	"github.com/bdsplatform/platform/backend/libs/logger"
	"github.com/bdsplatform/platform/backend/libs/telemetry"
)

// Header and local key names.
const (
	HeaderCorrelationID = "X-Correlation-ID"
	HeaderRequestID     = "X-Request-ID"
	HeaderOrgID         = "X-Org-ID"

	localCorrelationID = "correlation_id"
)

// CorrelationID ensures every request has a correlation ID, echoes it on the
// response, and stores it in the request context and locals.
func CorrelationID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := firstNonEmpty(c.Get(HeaderCorrelationID), c.Get(HeaderRequestID))
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(HeaderCorrelationID, id)
		c.Locals(localCorrelationID, id)
		c.SetUserContext(logger.WithCorrelationID(c.UserContext(), id))

		fmt.Printf("\n=== MIDDLEWARE ENTER ===\nmiddleware=CorrelationID\nmethod=%s\npath=%s\ncorrelation_id=%s\n", c.Method(), c.Path(), id)

		err := c.Next()

		fmt.Printf("\n=== MIDDLEWARE EXIT ===\nmiddleware=CorrelationID\nstatus=%d\nerror=%v\n", c.Response().StatusCode(), err)

		return err
	}
}

// Tracing starts a server span per request, extracting any inbound trace context
// and recording the response status.
func Tracing(serviceName string) fiber.Handler {
	tracer := telemetry.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(c *fiber.Ctx) error {
		carrier := &fiberCarrier{c: c}
		ctx := propagator.Extract(c.UserContext(), carrier)

		ctx, span := tracer.Start(ctx, c.Method()+" "+c.Path())
		defer span.End()
		c.SetUserContext(ctx)

		corrID, _ := c.Locals(localCorrelationID).(string)
		fmt.Printf("\n=== MIDDLEWARE ENTER ===\nmiddleware=Tracing\nmethod=%s\npath=%s\ncorrelation_id=%s\n", c.Method(), c.Path(), corrID)

		err := c.Next()

		status := c.Response().StatusCode()
		span.SetAttributes(
			attribute.String("http.method", c.Method()),
			attribute.String("http.route", c.Path()),
			attribute.Int("http.status_code", status),
		)
		if err != nil || status >= 500 {
			span.SetStatus(codes.Error, "request failed")
			if err != nil {
				span.RecordError(err)
			}
		}

		fmt.Printf("\n=== MIDDLEWARE EXIT ===\nmiddleware=Tracing\nstatus=%d\nerror=%v\n", status, err)

		return err
	}
}

// Tenant extracts the organization ID (injected by the gateway) into the context
// so it can scope database access and authorization.
func Tenant() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if orgID := c.Get(HeaderOrgID); orgID != "" {
			c.SetUserContext(authz.WithOrg(c.UserContext(), orgID))
		}
		return c.Next()
	}
}

// RequestLogger logs one structured line per request and records HTTP metrics.
func RequestLogger(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		corrID, _ := c.Locals(localCorrelationID).(string)
		fmt.Printf("\n=== MIDDLEWARE ENTER ===\nmiddleware=RequestLogger\nmethod=%s\npath=%s\ncorrelation_id=%s\n", c.Method(), c.Path(), corrID)

		start := time.Now()
		err := c.Next()
		latency := time.Since(start)

		status := c.Response().StatusCode()
		route := c.Route().Path
		if route == "" {
			route = c.Path()
		}

		telemetry.HTTPRequests.WithLabelValues(c.Method(), route, statusClass(status)).Inc()
		telemetry.HTTPDuration.WithLabelValues(c.Method(), route).Observe(latency.Seconds())

		log.InfoContext(c.UserContext(), "http_request",
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("ip", c.IP()),
		)

		fmt.Printf("\n=== MIDDLEWARE EXIT ===\nmiddleware=RequestLogger\nstatus=%d\nerror=%v\n", status, err)

		return err
	}
}

// Recover converts panics into INTERNAL errors and logs them.
func Recover(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		corrID, _ := c.Locals(localCorrelationID).(string)
		fmt.Printf("\n=== MIDDLEWARE ENTER ===\nmiddleware=Recover\nmethod=%s\npath=%s\ncorrelation_id=%s\n", c.Method(), c.Path(), corrID)

		defer func() {
			if r := recover(); r != nil {
				log.ErrorContext(c.UserContext(), "panic recovered", slog.Any("panic", r))
				err = errors.Internal("internal server error")
			}
			fmt.Printf("\n=== MIDDLEWARE EXIT ===\nmiddleware=Recover\nstatus=%d\nerror=%v\n", c.Response().StatusCode(), err)
		}()
		return c.Next()
	}
}

// ErrorHandler renders any error as the standard API error envelope.
func ErrorHandler() fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		appErr := coerce(err)
		requestID, _ := c.Locals(localCorrelationID).(string)
		return c.Status(appErr.HTTPStatus()).JSON(appErr.Envelope(requestID))
	}
}

func coerce(err error) *errors.Error {
	if fe, ok := err.(*fiber.Error); ok {
		code := errors.CodeInternal
		switch fe.Code {
		case fiber.StatusNotFound:
			code = errors.CodeNotFound
		case fiber.StatusUnauthorized:
			code = errors.CodeUnauthenticated
		case fiber.StatusForbidden:
			code = errors.CodeForbidden
		case fiber.StatusTooManyRequests:
			code = errors.CodeRateLimited
		}
		return errors.New(code, fe.Message)
	}
	return errors.From(err)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

// fiberCarrier adapts a Fiber context to the OpenTelemetry TextMapCarrier so
// trace context can be extracted from request headers and injected into the
// response.
type fiberCarrier struct{ c *fiber.Ctx }

func (f *fiberCarrier) Get(key string) string { return f.c.Get(key) }
func (f *fiberCarrier) Set(key, val string)   { f.c.Set(key, val) }
func (f *fiberCarrier) Keys() []string        { return nil }
