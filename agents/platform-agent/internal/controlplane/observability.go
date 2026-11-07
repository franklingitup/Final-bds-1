package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// IngestMetricsRequest is the request to ingest metrics.
type IngestMetricsRequest struct {
	ClusterID string             `json:"clusterId"`
	Metrics   []IngestMetricData `json:"metrics"`
}

// IngestMetricData is a single metric data point.
type IngestMetricData struct {
	Name         string            `json:"name"`
	ResourceType string            `json:"resourceType"`
	ResourceID   string            `json:"resourceId"`
	Labels       map[string]string `json:"labels,omitempty"`
	Value        float64           `json:"value"`
	Timestamp    int64             `json:"timestamp,omitempty"` // Unix ms
}

// IngestLogsRequest is the request to ingest logs.
type IngestLogsRequest struct {
	ClusterID string           `json:"clusterId"`
	Streams   []IngestLogEntry `json:"streams"`
}

// IngestLogEntry is a batch of log lines (Loki-compatible format).
type IngestLogEntry struct {
	// Stream labels (namespace, pod, container, deployment_id, etc.)
	Stream map[string]string `json:"stream"`
	// Values are [[timestamp_ns_string, line], ...]
	// Timestamp must be nanosecond Unix timestamp as a STRING (Loki requirement).
	Values [][]string `json:"values"`
}

// IngestMetrics sends metrics to the observability service.
// Uses agent credential authentication (X-Cluster-ID, X-Agent-ID headers).
func (c *Client) IngestMetrics(ctx context.Context, creds AgentCredentials, req IngestMetricsRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/agent/metrics", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Cluster-ID", creds.ClusterID)
	httpReq.Header.Set("X-Agent-ID", creds.AgentID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content is the success response
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
		}
		return &APIError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	return nil
}

// IngestLogs sends logs to the observability service.
// Uses agent credential authentication (X-Cluster-ID, X-Agent-ID headers).
func (c *Client) IngestLogs(ctx context.Context, creds AgentCredentials, req IngestLogsRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/agent/logs", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Cluster-ID", creds.ClusterID)
	httpReq.Header.Set("X-Agent-ID", creds.AgentID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content is the success response
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
		}
		return &APIError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	return nil
}
