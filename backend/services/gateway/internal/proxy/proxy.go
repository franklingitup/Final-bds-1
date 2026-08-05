// Package proxy provides reverse proxy functionality for routing requests
// to backend services.
package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// ServiceConfig defines configuration for a backend service.
type ServiceConfig struct {
	Name        string
	BaseURL     string
	Timeout     time.Duration
	StripPrefix string // Path prefix to strip before forwarding.
}

// Service represents a backend service that can be proxied to.
type Service struct {
	config ServiceConfig
	client *http.Client
	base   *url.URL
	log    *slog.Logger
}

// NewService creates a new proxy service.
func NewService(cfg ServiceConfig, log *slog.Logger) (*Service, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL %q: %w", cfg.BaseURL, err)
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Service{
		config: cfg,
		base:   base,
		client: &http.Client{Timeout: timeout},
		log:    log.With(slog.String("proxy_target", cfg.Name)),
	}, nil
}

// Name returns the service name.
func (s *Service) Name() string {
	return s.config.Name
}

// Handler returns a Fiber handler that proxies requests to this service.
func (s *Service) Handler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return s.proxy(c)
	}
}

func (s *Service) proxy(c *fiber.Ctx) error {
	ctx := c.UserContext()
	bodyBytes := c.Body()

	// Build target URL.
	path := c.Path()
	if s.config.StripPrefix != "" {
		path = strings.TrimPrefix(path, s.config.StripPrefix)
		if path == "" {
			path = "/"
		}
	}

	targetURL := *s.base
	targetURL.Path = path
	targetURL.RawQuery = string(c.Request().URI().QueryString())

	// Create the outbound request.
	req, err := http.NewRequestWithContext(ctx, c.Method(), targetURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("proxy: create request: %w", err)
	}

	// Forward headers.
	c.Request().Header.VisitAll(func(key, value []byte) {
		k := string(key)
		if isHopByHop(k) {
			return
		}
		req.Header.Add(k, string(value))
	})

	// Set/override forwarding headers.
	req.Header.Set("X-Forwarded-For", c.IP())
	req.Header.Set("X-Forwarded-Proto", c.Protocol())
	req.Header.Set("X-Forwarded-Host", string(c.Request().Host()))
	req.Header.Set("X-Real-IP", c.IP())

	// Make the request.
	resp, err := s.client.Do(req)
	if err != nil {
		s.log.ErrorContext(ctx, "proxy request failed",
			slog.String("method", c.Method()),
			slog.String("path", path),
			slog.String("error", err.Error()),
		)
		return fiber.NewError(fiber.StatusBadGateway, "upstream service unavailable")
	}
	defer resp.Body.Close()

	// Read response body.
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("proxy: read upstream body: %w", readErr)
	}

	// Copy response headers, skipping Content-Length (Fiber will set it).
	for k, values := range resp.Header {
		if isHopByHop(k) || k == "Content-Length" {
			continue
		}
		for _, v := range values {
			c.Response().Header.Set(k, v)
		}
	}

	c.Status(resp.StatusCode)
	return c.Send(respBody)
}

// hopByHop headers that should not be forwarded.
var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailers":            true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func isHopByHop(header string) bool {
	return hopByHopHeaders[http.CanonicalHeaderKey(header)]
}

// HealthCheck performs a health check against the service.
func (s *Service) HealthCheck(ctx context.Context) error {
	healthURL := *s.base
	healthURL.Path = "/healthz"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL.String(), nil)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}
