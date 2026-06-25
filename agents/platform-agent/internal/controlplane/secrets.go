package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Secret represents a secret retrieved from the control plane.
type Secret struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Value     string `json:"value"`
	Version   int64  `json:"version"`
}

// SecretsResponse is the response from the secrets endpoint.
type SecretsResponse struct {
	Secrets []Secret `json:"secrets"`
}

// GetSecrets retrieves secrets for deployments on the cluster.
// This uses the agent's cluster credentials for authentication.
func (c *Client) GetSecrets(ctx context.Context, creds AgentCredentials) ([]Secret, error) {
	url := fmt.Sprintf("%s/v1/agent/clusters/%s/secrets", c.baseURL, creds.ClusterID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Add agent credentials.
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

	var result SecretsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result.Secrets, nil
}
