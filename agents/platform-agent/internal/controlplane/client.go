// Package controlplane provides a client for communicating with the control plane.
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the control plane API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new control plane client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// RegisterRequest is sent during agent registration.
type RegisterRequest struct {
	Token             string `json:"token"`
	AgentID           string `json:"agentId"`
	KubernetesVersion string `json:"kubernetesVersion"`
	NodeCount         int    `json:"nodeCount"`
	CloudProvider     string `json:"cloudProvider,omitempty"`
	Region            string `json:"region,omitempty"`
}

// RegisterResponse is returned from successful registration.
type RegisterResponse struct {
	ID                string    `json:"id"`
	OrganizationID    string    `json:"organizationId"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	Status            string    `json:"status"`
	KubernetesVersion string    `json:"kubernetesVersion"`
	NodeCount         *int      `json:"nodeCount"`
	CloudProvider     string    `json:"cloudProvider"`
	Region            string    `json:"region"`
	AgentID           string    `json:"agentId"`
	RegisteredAt      time.Time `json:"registeredAt"`
}

// Register registers the agent with the control plane using the registration token.
func (c *Client) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return nil, &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var result RegisterResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result, nil
}

// Recover fetches the cluster bound to the installation token from the control
// plane so an agent that lost its local state can rebuild it without a new
// token. It calls GET /v1/agent/recover, authenticating solely by possession of
// the installation token (passed via the X-Registration-Token header). The
// response reuses RegisterResponse; the returned AgentID is the authoritative,
// stable identity the agent should adopt.
func (c *Client) Recover(ctx context.Context, token, agentID string) (*RegisterResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/agent/recover", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Registration-Token", token)
	if agentID != "" {
		httpReq.Header.Set("X-Agent-ID", agentID)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return nil, &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var result RegisterResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result, nil
}

// HeartbeatRequest is sent periodically by the agent.
type HeartbeatRequest struct {
	AgentID           string `json:"agentId"`
	KubernetesVersion string `json:"kubernetesVersion"`
	NodeCount         int    `json:"nodeCount"`
	APIServerHealthy  bool   `json:"apiServerHealthy"`
}

// HeartbeatResponse is returned from a successful heartbeat.
type HeartbeatResponse struct {
	Status string `json:"status"`
}

// HeartbeatWithCreds sends a heartbeat using agent credential authentication.
// This uses the dedicated agent heartbeat endpoint with X-Cluster-ID and X-Agent-ID headers.
func (c *Client) HeartbeatWithCreds(ctx context.Context, creds AgentCredentials, req HeartbeatRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/agent/clusters/%s/heartbeat", c.baseURL, creds.ClusterID)
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

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
		}
		return &APIError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	return nil
}

// Heartbeat sends a heartbeat to the control plane.
// Deprecated: Use HeartbeatWithCreds instead.
func (c *Client) Heartbeat(ctx context.Context, orgID, clusterID string, req HeartbeatRequest) error {
	creds := AgentCredentials{
		ClusterID: clusterID,
		AgentID:   req.AgentID,
	}
	return c.HeartbeatWithCreds(ctx, creds, req)
}

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// APIError represents an error from the control plane API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("control plane error (status %d): %s", e.StatusCode, e.Message)
}

// IsUnauthorized returns true if the error is a 401 unauthorized.
func (e *APIError) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized
}

// IsConflict returns true if the error is a 409 conflict (e.g., token already used).
func (e *APIError) IsConflict() bool {
	return e.StatusCode == http.StatusConflict
}

// IsForbidden returns true if the error is a 403 forbidden.
func (e *APIError) IsForbidden() bool {
	return e.StatusCode == http.StatusForbidden
}

// IsNotFound returns true if the error is a 404 not found (e.g., the control
// plane no longer has a record of the cluster).
func (e *APIError) IsNotFound() bool {
	return e.StatusCode == http.StatusNotFound
}
