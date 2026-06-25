package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DesiredDeployment represents a deployment the agent should reconcile.
// This struct matches the response from the dedicated agent desired state API.
type DesiredDeployment struct {
	DeploymentID    string `json:"deploymentId"`
	ReleaseID       string `json:"releaseId"`
	ApplicationID   string `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
	ApplicationSlug string `json:"applicationSlug"`
	Namespace       string `json:"namespace,omitempty"`
	Image           string `json:"image"`
	Revision        int    `json:"revision"`
	Replicas        int    `json:"replicas"`
	Port            *int   `json:"port,omitempty"`

	EnvVars          []EnvVar      `json:"envVars,omitempty"`
	ResourceRequests *ResourceSpec `json:"resourceRequests,omitempty"`
	ResourceLimits   *ResourceSpec `json:"resourceLimits,omitempty"`

	Status string `json:"status"`
}

// EnvVar represents an environment variable.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ResourceSpec represents CPU/memory resources.
type ResourceSpec struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// DesiredStateResponse is returned from the agent desired state endpoint.
type DesiredStateResponse struct {
	ClusterID  string              `json:"clusterId"`
	Items      []DesiredDeployment `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

// AgentCredentials holds the credentials for agent authentication.
type AgentCredentials struct {
	ClusterID string
	AgentID   string
}

// GetDesiredState fetches the desired deployment state using the dedicated agent endpoint.
// This uses cluster credential authentication (X-Cluster-ID, X-Agent-ID headers).
func (c *Client) GetDesiredState(ctx context.Context, creds AgentCredentials) ([]DesiredDeployment, error) {
	url := fmt.Sprintf("%s/v1/agent/clusters/%s/desired-state", c.baseURL, creds.ClusterID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("X-Cluster-ID", creds.ClusterID)
	httpReq.Header.Set("X-Agent-ID", creds.AgentID)

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

	var result DesiredStateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result.Items, nil
}

// GetDesiredDeployments is deprecated. Use GetDesiredState instead.
// This method is kept for backward compatibility and wraps GetDesiredState.
func (c *Client) GetDesiredDeployments(ctx context.Context, orgID, clusterID, accessToken string) ([]DesiredDeployment, error) {
	// Extract agent ID from access token if possible, otherwise use a placeholder.
	// In practice, callers should migrate to GetDesiredState with proper credentials.
	creds := AgentCredentials{
		ClusterID: clusterID,
		AgentID:   accessToken, // Legacy: use access token as agent ID
	}
	return c.GetDesiredState(ctx, creds)
}

// Canonical release status values for agent-to-control-plane reporting.
// These MUST match backend/libs/contracts/deploymentstatus.
const (
	// StatusDeploying indicates the release is actively being deployed.
	StatusDeploying = "deploying"
	// StatusSucceeded indicates the release completed successfully.
	StatusSucceeded = "succeeded"
	// StatusFailed indicates the release failed.
	StatusFailed = "failed"
)

// DeploymentStatusRequest reports deployment status to the control plane.
type DeploymentStatusRequest struct {
	Status        string  `json:"status"` // deploying, succeeded, failed
	ReadyReplicas *int    `json:"readyReplicas,omitempty"`
	ErrorMessage  *string `json:"errorMessage,omitempty"`
}

// ReportDeploymentStatusWithCreds reports deployment status using agent credentials.
// This uses the dedicated agent status endpoint with cluster credential authentication.
func (c *Client) ReportDeploymentStatusWithCreds(ctx context.Context, creds AgentCredentials, deploymentID, releaseID string, req DeploymentStatusRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/agent/deployments/%s/releases/%s/status", c.baseURL, deploymentID, releaseID)
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

// ReportDeploymentStatus is deprecated. Use ReportDeploymentStatusWithCreds instead.
// This method is kept for backward compatibility.
func (c *Client) ReportDeploymentStatus(ctx context.Context, orgID, deploymentID, releaseID, accessToken string, req DeploymentStatusRequest) error {
	creds := AgentCredentials{
		ClusterID: orgID,     // Legacy: use orgID as clusterID
		AgentID:   accessToken, // Legacy: use access token as agent ID
	}
	return c.ReportDeploymentStatusWithCreds(ctx, creds, deploymentID, releaseID, req)
}
