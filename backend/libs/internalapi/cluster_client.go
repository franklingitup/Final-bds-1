// Package internalapi provides HTTP clients for service-to-service calls.
package internalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ClusterClient calls the cluster service's internal APIs.
type ClusterClient struct {
	baseURL       string
	InternalToken string
	http          *http.Client
}

// NewClusterClient builds a client for the cluster service at baseURL
// (e.g. http://localhost:8085). The HTTP timeout defaults to DefaultTimeout.
func NewClusterClient(baseURL string) *ClusterClient {
	return &ClusterClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: DefaultTimeout},
	}
}

// CreateClusterRequest is the request to create a cluster via internal API.
type CreateClusterRequest struct {
	Name          string            `json:"name"`
	Slug          string            `json:"slug"`
	Description   string            `json:"description,omitempty"`
	CloudProvider string            `json:"cloudProvider,omitempty"`
	Region        string            `json:"region,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	// CreatedBy is the user ID creating the cluster (for audit purposes).
	CreatedBy string `json:"createdBy"`
}

// CreateClusterResponse is the response from creating a cluster.
type CreateClusterResponse struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Status            string `json:"status"`
	RegistrationToken string `json:"registrationToken"`
	ManifestURL       string `json:"manifestUrl"`
	InstallCommand    string `json:"installCommand"`
	ExpiresAt         string `json:"expiresAt"`
}

// ClusterStatus is the internal lifecycle projection used by provisioning.
type ClusterStatus struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	AgentID           string `json:"agentId,omitempty"`
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
}

// ClusterCreator is the interface for internal cluster lifecycle operations.
// This allows tests to substitute a fake without making HTTP calls.
type ClusterCreator interface {
	CreateClusterWithToken(ctx context.Context, orgID string, req CreateClusterRequest) (*CreateClusterResponse, error)
	GetCluster(ctx context.Context, orgID, clusterID string) (*ClusterStatus, error)
}

// CreateClusterWithToken creates a cluster and generates a registration token
// in a single atomic operation. This is used by the provisioning service to
// create real cluster records before Terraform provisioning begins.
//
// The operation is atomic: either both the cluster and token are created, or
// neither is. This prevents orphaned clusters without valid tokens.
func (c *ClusterClient) CreateClusterWithToken(ctx context.Context, orgID string, req CreateClusterRequest) (*CreateClusterResponse, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("internalapi: cluster client not configured")
	}
	if orgID == "" {
		return nil, fmt.Errorf("internalapi: orgID is required")
	}

	endpoint := fmt.Sprintf("%s/internal/organizations/%s/clusters", c.baseURL, orgID)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("internalapi: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("internalapi: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.InternalToken != "" {
		httpReq.Header.Set(HeaderInternalToken, c.InternalToken)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("internalapi: create cluster: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("internalapi: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract error message from response
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("internalapi: create cluster failed: %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("internalapi: create cluster: status %d", resp.StatusCode)
	}

	var result CreateClusterResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("internalapi: decode response: %w", err)
	}

	return &result, nil
}

// GetCluster returns the cluster lifecycle state used to synchronize a
// provisioning install session with real agent registration.
func (c *ClusterClient) GetCluster(ctx context.Context, orgID, clusterID string) (*ClusterStatus, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("internalapi: cluster client not configured")
	}
	if orgID == "" || clusterID == "" {
		return nil, fmt.Errorf("internalapi: orgID and clusterID are required")
	}

	endpoint := fmt.Sprintf("%s/internal/organizations/%s/clusters/%s", c.baseURL, orgID, clusterID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("internalapi: new request: %w", err)
	}
	if c.InternalToken != "" {
		req.Header.Set(HeaderInternalToken, c.InternalToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("internalapi: get cluster: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("internalapi: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("internalapi: get cluster: status %d", resp.StatusCode)
	}

	var result ClusterStatus
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("internalapi: decode cluster: %w", err)
	}
	return &result, nil
}
