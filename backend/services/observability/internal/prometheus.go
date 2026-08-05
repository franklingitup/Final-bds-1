package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// PrometheusConfig holds Prometheus client configuration.
type PrometheusConfig struct {
	URL     string
	Timeout time.Duration
}

// DefaultPrometheusConfig returns default configuration.
func DefaultPrometheusConfig() PrometheusConfig {
	return PrometheusConfig{
		URL:     "http://localhost:9090",
		Timeout: 30 * time.Second,
	}
}

// PrometheusClient queries Prometheus for metrics.
type PrometheusClient struct {
	baseURL string
	client  *http.Client
}

// NewPrometheusClient creates a new Prometheus client.
func NewPrometheusClient(cfg PrometheusConfig) *PrometheusClient {
	return &PrometheusClient{
		baseURL: cfg.URL,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Query executes an instant query.
func (c *PrometheusClient) Query(ctx context.Context, query string, ts time.Time) (*MetricsResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	if !ts.IsZero() {
		params.Set("time", fmt.Sprintf("%d", ts.Unix()))
	}

	resp, err := c.doRequest(ctx, "/api/v1/query", params)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// QueryRange executes a range query.
func (c *PrometheusClient) QueryRange(ctx context.Context, query string, start, end time.Time, step string) (*MetricsResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", fmt.Sprintf("%d", start.Unix()))
	params.Set("end", fmt.Sprintf("%d", end.Unix()))
	if step != "" {
		params.Set("step", step)
	} else {
		params.Set("step", "60s")
	}

	resp, err := c.doRequest(ctx, "/api/v1/query_range", params)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Series returns time series matching a label set.
func (c *PrometheusClient) Series(ctx context.Context, match []string, start, end time.Time) ([]map[string]string, error) {
	params := url.Values{}
	for _, m := range match {
		params.Add("match[]", m)
	}
	params.Set("start", fmt.Sprintf("%d", start.Unix()))
	params.Set("end", fmt.Sprintf("%d", end.Unix()))

	u := fmt.Sprintf("%s/api/v1/series?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prometheus error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}

	return result.Data, nil
}

// Labels returns label values for a given label name.
func (c *PrometheusClient) Labels(ctx context.Context, labelName string, start, end time.Time) ([]string, error) {
	params := url.Values{}
	params.Set("start", fmt.Sprintf("%d", start.Unix()))
	params.Set("end", fmt.Sprintf("%d", end.Unix()))

	u := fmt.Sprintf("%s/api/v1/label/%s/values?%s", c.baseURL, url.PathEscape(labelName), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prometheus error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}

	return result.Data, nil
}

// Health checks Prometheus availability.
func (c *PrometheusClient) Health(ctx context.Context) error {
	u := fmt.Sprintf("%s/-/healthy", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("prometheus health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus unhealthy: status %d", resp.StatusCode)
	}
	return nil
}

func (c *PrometheusClient) doRequest(ctx context.Context, path string, params url.Values) (*MetricsResponse, error) {
	u := fmt.Sprintf("%s%s?%s", c.baseURL, path, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prometheus error %d: %s", resp.StatusCode, string(body))
	}

	var result MetricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode prometheus response: %w", err)
	}

	return &result, nil
}

// Standard Prometheus queries for platform metrics.
var StandardQueries = map[string]string{
	// Cluster metrics
	"cluster_cpu_usage":    `sum(rate(container_cpu_usage_seconds_total{cluster="%s"}[5m])) * 100`,
	"cluster_memory_usage": `sum(container_memory_usage_bytes{cluster="%s"}) / sum(machine_memory_bytes{cluster="%s"}) * 100`,
	"cluster_pod_count":    `count(kube_pod_info{cluster="%s"})`,
	"cluster_node_count":   `count(kube_node_info{cluster="%s"})`,

	// Deployment metrics
	"deployment_replicas":        `kube_deployment_status_replicas{deployment="%s",namespace="%s"}`,
	"deployment_ready_replicas":  `kube_deployment_status_replicas_ready{deployment="%s",namespace="%s"}`,
	"deployment_cpu_usage":       `sum(rate(container_cpu_usage_seconds_total{pod=~"%s-.*",namespace="%s"}[5m])) * 100`,
	"deployment_memory_usage":    `sum(container_memory_usage_bytes{pod=~"%s-.*",namespace="%s"})`,
	"deployment_restart_count":   `sum(kube_pod_container_status_restarts_total{pod=~"%s-.*",namespace="%s"})`,

	// Application metrics
	"app_request_rate":     `sum(rate(http_requests_total{app="%s"}[5m]))`,
	"app_error_rate":       `sum(rate(http_requests_total{app="%s",status=~"5.."}[5m])) / sum(rate(http_requests_total{app="%s"}[5m])) * 100`,
	"app_latency_p50":      `histogram_quantile(0.50, sum(rate(http_request_duration_seconds_bucket{app="%s"}[5m])) by (le))`,
	"app_latency_p95":      `histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{app="%s"}[5m])) by (le))`,
	"app_latency_p99":      `histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{app="%s"}[5m])) by (le))`,

	// Node metrics
	"node_cpu_usage":      `100 - (avg(rate(node_cpu_seconds_total{mode="idle",node="%s"}[5m])) * 100)`,
	"node_memory_usage":   `(1 - node_memory_MemAvailable_bytes{node="%s"} / node_memory_MemTotal_bytes{node="%s"}) * 100`,
	"node_disk_usage":     `(1 - node_filesystem_avail_bytes{node="%s",mountpoint="/"} / node_filesystem_size_bytes{node="%s",mountpoint="/"}) * 100`,
	"node_network_rx":     `rate(node_network_receive_bytes_total{node="%s",device!="lo"}[5m])`,
	"node_network_tx":     `rate(node_network_transmit_bytes_total{node="%s",device!="lo"}[5m])`,
}

// BuildQuery builds a Prometheus query from a template.
func BuildQuery(template string, args ...interface{}) string {
	return fmt.Sprintf(template, args...)
}
