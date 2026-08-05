package argocd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the OpenTelemetry instrumentation scope for this client.
const tracerName = "libs/argocd"

// ErrNotFound is returned when Argo CD reports the application does not exist.
var ErrNotFound = errors.New("argocd: application not found")

// APIError is a non-2xx response from the Argo CD API server.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("argocd: api error %d: %s", e.StatusCode, e.Message)
}

// Client talks to the Argo CD API server.
type Client interface {
	CreateApplication(ctx context.Context, app *Application) (*Application, error)
	UpdateApplication(ctx context.Context, app *Application) (*Application, error)
	DeleteApplication(ctx context.Context, name string, cascade bool) error
	GetApplication(ctx context.Context, name string) (*Application, error)
	RefreshApplication(ctx context.Context, name string, hard bool) (*Application, error)
	ListApplications(ctx context.Context, opts ListOptions) ([]Application, error)
	SyncApplication(ctx context.Context, name string, req SyncRequest) (*Application, error)
	TerminateOperation(ctx context.Context, name string) error
	RollbackApplication(ctx context.Context, name string, historyID int64) (*Application, error)
	WaitForSync(ctx context.Context, name string, opts WaitOptions) (*Application, error)
	WaitForHealthy(ctx context.Context, name string, opts WaitOptions) (*Application, error)
	// RetryOperation re-issues fn until it succeeds, ctx is cancelled, or the
	// attempt budget is exhausted, applying capped exponential backoff. It is
	// the shared building block WaitFor* and callers use for transient Argo CD
	// API errors.
	RetryOperation(ctx context.Context, opts RetryOptions, fn func(context.Context) error) error
}

// ListOptions filters ListApplications.
type ListOptions struct {
	// Selector is a Kubernetes label selector, e.g. "app.kubernetes.io/managed-by=bdsplatform".
	Selector string
	// Project restricts results to a single Argo CD project.
	Project string
}

// WaitOptions bounds a WaitForSync / WaitForHealthy poll loop.
type WaitOptions struct {
	// Interval between polls. Defaults to 2s.
	Interval time.Duration
	// Timeout for the whole wait. Zero means rely on ctx only.
	Timeout time.Duration
	// Refresh requests a repo refresh on each poll so the comparison is current.
	Refresh bool
}

// RetryOptions bounds RetryOperation.
type RetryOptions struct {
	// MaxAttempts including the first try. Defaults to 3.
	MaxAttempts int
	// BaseDelay is the first backoff. Defaults to 200ms.
	BaseDelay time.Duration
	// MaxDelay caps the backoff. Defaults to 5s.
	MaxDelay time.Duration
}

// Config configures an HTTP-backed Client.
type Config struct {
	// BaseURL of the Argo CD API server, e.g. https://argocd.example.com.
	BaseURL string
	// AuthToken is the bearer token (Argo CD account / API key token).
	AuthToken string
	// Insecure skips TLS verification (self-signed argocd-server). Off by default.
	Insecure bool
	// Timeout for a single HTTP request. Defaults to 30s.
	Timeout time.Duration
	// HTTPClient overrides the default client (tests inject an httptest client).
	HTTPClient *http.Client
	// Logger for structured logs. Defaults to slog.Default().
	Logger *slog.Logger
}

type httpClient struct {
	base   *url.URL
	token  string
	http   *http.Client
	log    *slog.Logger
	tracer trace.Tracer
}

// New constructs an HTTP-backed Argo CD Client. It returns an error when the
// base URL is missing or unparseable.
func New(cfg Config) (Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("argocd: BaseURL is required")
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("argocd: invalid BaseURL: %w", err)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	hc := cfg.HTTPClient
	if hc == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.Insecure {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for self-signed argocd-server
		}
		hc = &http.Client{Timeout: timeout, Transport: transport}
	}
	return &httpClient{
		base:   base,
		token:  cfg.AuthToken,
		http:   hc,
		log:    cfg.Logger,
		tracer: otel.Tracer(tracerName),
	}, nil
}

func (c *httpClient) CreateApplication(ctx context.Context, app *Application) (*Application, error) {
	ctx, span := c.tracer.Start(ctx, "argocd.CreateApplication")
	defer span.End()
	if app != nil {
		span.SetAttributes(attribute.String("argocd.application", app.Metadata.Name))
	}
	var out Application
	// upsert=true makes create idempotent: repeated creates update in place
	// rather than failing with AlreadyExists.
	if err := c.do(ctx, http.MethodPost, "/api/v1/applications?upsert=true", app, &out); err != nil {
		return nil, c.trace(span, err)
	}
	c.log.InfoContext(ctx, "argocd application created", "application", out.Metadata.Name)
	return &out, nil
}

func (c *httpClient) UpdateApplication(ctx context.Context, app *Application) (*Application, error) {
	ctx, span := c.tracer.Start(ctx, "argocd.UpdateApplication")
	defer span.End()
	if app == nil || app.Metadata.Name == "" {
		return nil, errors.New("argocd: application name is required for update")
	}
	span.SetAttributes(attribute.String("argocd.application", app.Metadata.Name))
	var out Application
	path := "/api/v1/applications/" + url.PathEscape(app.Metadata.Name)
	if err := c.do(ctx, http.MethodPut, path, app, &out); err != nil {
		return nil, c.trace(span, err)
	}
	c.log.InfoContext(ctx, "argocd application updated", "application", out.Metadata.Name)
	return &out, nil
}

func (c *httpClient) DeleteApplication(ctx context.Context, name string, cascade bool) error {
	ctx, span := c.tracer.Start(ctx, "argocd.DeleteApplication")
	defer span.End()
	span.SetAttributes(attribute.String("argocd.application", name))
	path := fmt.Sprintf("/api/v1/applications/%s?cascade=%t", url.PathEscape(name), cascade)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return c.trace(span, err)
	}
	c.log.InfoContext(ctx, "argocd application deleted", "application", name, "cascade", cascade)
	return nil
}

func (c *httpClient) GetApplication(ctx context.Context, name string) (*Application, error) {
	ctx, span := c.tracer.Start(ctx, "argocd.GetApplication")
	defer span.End()
	span.SetAttributes(attribute.String("argocd.application", name))
	var out Application
	if err := c.do(ctx, http.MethodGet, "/api/v1/applications/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, c.trace(span, err)
	}
	return &out, nil
}

func (c *httpClient) RefreshApplication(ctx context.Context, name string, hard bool) (*Application, error) {
	ctx, span := c.tracer.Start(ctx, "argocd.RefreshApplication")
	defer span.End()
	span.SetAttributes(attribute.String("argocd.application", name))
	mode := "normal"
	if hard {
		mode = "hard"
	}
	path := fmt.Sprintf("/api/v1/applications/%s?refresh=%s", url.PathEscape(name), mode)
	var out Application
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, c.trace(span, err)
	}
	return &out, nil
}

func (c *httpClient) ListApplications(ctx context.Context, opts ListOptions) ([]Application, error) {
	ctx, span := c.tracer.Start(ctx, "argocd.ListApplications")
	defer span.End()
	q := url.Values{}
	if opts.Selector != "" {
		q.Set("selector", opts.Selector)
	}
	if opts.Project != "" {
		q.Set("projects", opts.Project)
	}
	path := "/api/v1/applications"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out applicationList
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, c.trace(span, err)
	}
	span.SetAttributes(attribute.Int("argocd.applications.count", len(out.Items)))
	return out.Items, nil
}

func (c *httpClient) SyncApplication(ctx context.Context, name string, req SyncRequest) (*Application, error) {
	ctx, span := c.tracer.Start(ctx, "argocd.SyncApplication")
	defer span.End()
	span.SetAttributes(
		attribute.String("argocd.application", name),
		attribute.String("argocd.sync.revision", req.Revision),
	)
	var out Application
	path := "/api/v1/applications/" + url.PathEscape(name) + "/sync"
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, c.trace(span, err)
	}
	c.log.InfoContext(ctx, "argocd sync requested", "application", name, "revision", req.Revision)
	return &out, nil
}

func (c *httpClient) TerminateOperation(ctx context.Context, name string) error {
	ctx, span := c.tracer.Start(ctx, "argocd.TerminateOperation")
	defer span.End()
	span.SetAttributes(attribute.String("argocd.application", name))
	path := "/api/v1/applications/" + url.PathEscape(name) + "/operation"
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return c.trace(span, err)
	}
	c.log.InfoContext(ctx, "argocd operation terminated", "application", name)
	return nil
}

func (c *httpClient) RollbackApplication(ctx context.Context, name string, historyID int64) (*Application, error) {
	ctx, span := c.tracer.Start(ctx, "argocd.RollbackApplication")
	defer span.End()
	span.SetAttributes(
		attribute.String("argocd.application", name),
		attribute.Int64("argocd.rollback.id", historyID),
	)
	var out Application
	path := "/api/v1/applications/" + url.PathEscape(name) + "/rollback"
	if err := c.do(ctx, http.MethodPost, path, RollbackRequest{ID: historyID}, &out); err != nil {
		return nil, c.trace(span, err)
	}
	c.log.InfoContext(ctx, "argocd rollback requested", "application", name, "historyId", historyID)
	return &out, nil
}

func (c *httpClient) WaitForSync(ctx context.Context, name string, opts WaitOptions) (*Application, error) {
	return c.waitFor(ctx, name, opts, "argocd.WaitForSync", func(a *Application) bool { return a.IsSynced() })
}

func (c *httpClient) WaitForHealthy(ctx context.Context, name string, opts WaitOptions) (*Application, error) {
	return c.waitFor(ctx, name, opts, "argocd.WaitForHealthy", func(a *Application) bool { return a.IsHealthy() })
}

func (c *httpClient) waitFor(ctx context.Context, name string, opts WaitOptions, spanName string, done func(*Application) bool) (*Application, error) {
	ctx, span := c.tracer.Start(ctx, spanName)
	defer span.End()
	span.SetAttributes(attribute.String("argocd.application", name))

	interval := opts.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		var app *Application
		var err error
		if opts.Refresh {
			app, err = c.RefreshApplication(ctx, name, false)
		} else {
			app, err = c.GetApplication(ctx, name)
		}
		if err != nil {
			// A not-found is terminal; other errors are treated as transient
			// and retried on the next tick.
			if errors.Is(err, ErrNotFound) {
				return nil, c.trace(span, err)
			}
			c.log.WarnContext(ctx, "argocd wait poll failed", "application", name, "error", err)
		} else if done(app) {
			return app, nil
		}

		select {
		case <-ctx.Done():
			return app, c.trace(span, fmt.Errorf("argocd: wait for %s timed out: %w", name, ctx.Err()))
		case <-ticker.C:
		}
	}
}

func (c *httpClient) RetryOperation(ctx context.Context, opts RetryOptions, fn func(context.Context) error) error {
	attempts := opts.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	base := opts.BaseDelay
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	maxDelay := opts.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 5 * time.Second
	}

	var err error
	delay := base
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = fn(ctx); err == nil {
			return nil
		}
		// Client errors (4xx except 429) are not retryable.
		if !retryable(err) || attempt == attempts {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay *= 2; delay > maxDelay {
			delay = maxDelay
		}
	}
	return err
}

// retryable reports whether an error is worth retrying: transport errors and
// 5xx / 429 responses are; other 4xx (bad request, forbidden, not found) are not.
func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 || apiErr.StatusCode == http.StatusTooManyRequests
	}
	// Non-API errors are transport-level (dial/timeout) and retryable.
	return !errors.Is(err, ErrNotFound)
}

// do performs a single request/response cycle, marshalling body (if any) and
// unmarshalling into out (if any). It maps 404 to ErrNotFound and other non-2xx
// to *APIError.
func (c *httpClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("argocd: marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, reader)
	if err != nil {
		return fmt.Errorf("argocd: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("argocd: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Message: extractMessage(respBody, resp.Status)}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("argocd: decode response: %w", err)
		}
	}
	return nil
}

// extractMessage pulls the "message" field from Argo CD's gRPC-gateway error
// envelope ({"error":"..","code":..,"message":".."}), falling back to raw text.
func extractMessage(body []byte, status string) string {
	if len(body) == 0 {
		return status
	}
	var env struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &env) == nil {
		if env.Message != "" {
			return env.Message
		}
		if env.Error != "" {
			return env.Error
		}
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return s
	}
	return status
}

// trace records err on the span and returns it unchanged, for one-line use.
func (c *httpClient) trace(span trace.Span, err error) error {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// FindHistoryID returns the most recent history entry ID whose revision matches
// the given git revision (SHA or ref). It returns false when no entry matches.
// This lets the deployment service translate a user-facing "rollback to revision
// X" request into the Argo CD history ID the rollback API requires.
func FindHistoryID(app *Application, revision string) (int64, bool) {
	if app == nil {
		return 0, false
	}
	var (
		found bool
		best  RevisionHistory
	)
	for _, h := range app.Status.History {
		if h.Revision == revision && (!found || h.ID > best.ID) {
			best = h
			found = true
		}
	}
	return best.ID, found
}

// ParseHistoryID is a small helper for handlers that accept a numeric history id
// as a string.
func ParseHistoryID(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
