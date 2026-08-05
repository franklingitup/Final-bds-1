package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// LokiConfig holds Loki client configuration.
type LokiConfig struct {
	URL     string
	Timeout time.Duration
}

// DefaultLokiConfig returns default configuration.
func DefaultLokiConfig() LokiConfig {
	return LokiConfig{
		URL:     "http://localhost:3100",
		Timeout: 30 * time.Second,
	}
}

// LokiClient queries Loki for logs.
type LokiClient struct {
	baseURL string
	client  *http.Client
}

// NewLokiClient creates a new Loki client.
func NewLokiClient(cfg LokiConfig) *LokiClient {
	return &LokiClient{
		baseURL: cfg.URL,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Query executes a LogQL query.
func (c *LokiClient) Query(ctx context.Context, query string, limit int, ts time.Time, direction string) (*LogsResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if !ts.IsZero() {
		params.Set("time", strconv.FormatInt(ts.UnixNano(), 10))
	}
	if direction != "" {
		params.Set("direction", direction)
	}

	return c.doQueryRequest(ctx, "/loki/api/v1/query", params)
}

// QueryRange executes a range LogQL query.
func (c *LokiClient) QueryRange(ctx context.Context, query string, limit int, start, end time.Time, step, direction string) (*LogsResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if step != "" {
		params.Set("step", step)
	}
	if direction != "" {
		params.Set("direction", direction)
	}

	return c.doQueryRequest(ctx, "/loki/api/v1/query_range", params)
}

// Labels returns all label names.
func (c *LokiClient) Labels(ctx context.Context, start, end time.Time) ([]string, error) {
	params := url.Values{}
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))

	u := fmt.Sprintf("%s/loki/api/v1/labels?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loki request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode loki response: %w", err)
	}

	return result.Data, nil
}

// LabelValues returns values for a label.
func (c *LokiClient) LabelValues(ctx context.Context, labelName string, start, end time.Time) ([]string, error) {
	params := url.Values{}
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))

	u := fmt.Sprintf("%s/loki/api/v1/label/%s/values?%s", c.baseURL, url.PathEscape(labelName), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loki request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode loki response: %w", err)
	}

	return result.Data, nil
}

// Series returns all streams matching a selector.
func (c *LokiClient) Series(ctx context.Context, match []string, start, end time.Time) ([]map[string]string, error) {
	params := url.Values{}
	for _, m := range match {
		params.Add("match[]", m)
	}
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))

	u := fmt.Sprintf("%s/loki/api/v1/series?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loki request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode loki response: %w", err)
	}

	return result.Data, nil
}

// Push sends log entries to Loki.
func (c *LokiClient) Push(ctx context.Context, streams []IngestLogEntry) error {
	body := struct {
		Streams []IngestLogEntry `json:"streams"`
	}{
		Streams: streams,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal push request: %w", err)
	}

	u := fmt.Sprintf("%s/loki/api/v1/push", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("loki push failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("loki push error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Health checks Loki availability.
func (c *LokiClient) Health(ctx context.Context) error {
	u := fmt.Sprintf("%s/ready", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("loki health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("loki unhealthy: status %d", resp.StatusCode)
	}
	return nil
}

func (c *LokiClient) doQueryRequest(ctx context.Context, path string, params url.Values) (*LogsResponse, error) {
	u := fmt.Sprintf("%s%s?%s", c.baseURL, path, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loki request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki error %d: %s", resp.StatusCode, string(body))
	}

	var result LogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode loki response: %w", err)
	}

	return &result, nil
}

// Standard LogQL queries for platform logs.
var StandardLogQueries = map[string]string{
	// Deployment logs
	"deployment_logs":       `{namespace="%s",app="%s"}`,
	"deployment_errors":     `{namespace="%s",app="%s"} |= "error" or |= "Error" or |= "ERROR"`,
	"deployment_warnings":   `{namespace="%s",app="%s"} |= "warn" or |= "Warn" or |= "WARN"`,

	// Cluster logs
	"cluster_events":        `{cluster="%s",stream="events"}`,
	"cluster_system_logs":   `{cluster="%s",namespace="kube-system"}`,

	// Application logs
	"app_logs":              `{app="%s"}`,
	"app_errors":            `{app="%s"} |= "error" or |= "Error" or |= "ERROR"`,

	// Pod logs
	"pod_logs":              `{namespace="%s",pod="%s"}`,
	"pod_container_logs":    `{namespace="%s",pod="%s",container="%s"}`,
}

// BuildLogQuery builds a LogQL query from a template.
func BuildLogQuery(template string, args ...interface{}) string {
	return fmt.Sprintf(template, args...)
}

// LogQueryBuilder helps build LogQL queries.
type LogQueryBuilder struct {
	selector string
	filters  []string
	parser   string
}

// NewLogQueryBuilder creates a new LogQL query builder.
func NewLogQueryBuilder(selector string) *LogQueryBuilder {
	return &LogQueryBuilder{selector: selector}
}

// Filter adds a line filter.
func (b *LogQueryBuilder) Filter(op, value string) *LogQueryBuilder {
	switch op {
	case "contains":
		b.filters = append(b.filters, fmt.Sprintf("|= %q", value))
	case "not_contains":
		b.filters = append(b.filters, fmt.Sprintf("!= %q", value))
	case "regex":
		b.filters = append(b.filters, fmt.Sprintf("|~ %q", value))
	case "not_regex":
		b.filters = append(b.filters, fmt.Sprintf("!~ %q", value))
	}
	return b
}

// JSON adds JSON parsing.
func (b *LogQueryBuilder) JSON() *LogQueryBuilder {
	b.parser = "| json"
	return b
}

// Logfmt adds logfmt parsing.
func (b *LogQueryBuilder) Logfmt() *LogQueryBuilder {
	b.parser = "| logfmt"
	return b
}

// Build returns the final LogQL query.
func (b *LogQueryBuilder) Build() string {
	query := b.selector
	for _, f := range b.filters {
		query += " " + f
	}
	if b.parser != "" {
		query += " " + b.parser
	}
	return query
}
