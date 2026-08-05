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

	// ProgressDeadlineSeconds optionally overrides the rollout progress deadline
	// the agent sets on the Kubernetes Deployment. Additive and optional: when
	// absent the agent applies k8s.DefaultProgressDeadlineSeconds.
	ProgressDeadlineSeconds *int `json:"progressDeadlineSeconds,omitempty"`

	// ConfigMaps are the ConfigMaps this deployment expects the agent to
	// reconcile alongside the workload. The field is additive and optional: a
	// control plane that does not send it leaves this nil, in which case the
	// agent reconciles no ConfigMaps (identical to previous behaviour).
	ConfigMaps []DesiredConfigMap `json:"configMaps,omitempty"`

	// PersistentVolumeClaims are the PVCs this deployment expects the agent to
	// reconcile. Like ConfigMaps this is additive and optional: absent field
	// leaves it nil and the agent reconciles no PVCs (previous behaviour).
	PersistentVolumeClaims []DesiredPVC `json:"persistentVolumeClaims,omitempty"`

	// Ingresses are the Ingress objects this deployment expects the agent to
	// reconcile. Additive and optional: absent field leaves it nil and the
	// agent reconciles no Ingresses (previous behaviour).
	Ingresses []DesiredIngress `json:"ingresses,omitempty"`

	Status string `json:"status"`
}

// DesiredIngress represents a Kubernetes Ingress the agent should reconcile as
// part of a deployment's desired state. Server-populated fields (status,
// loadBalancer) are never modeled.
type DesiredIngress struct {
	// Name is the Ingress object name. Required.
	Name string `json:"name"`
	// Namespace overrides the deployment namespace when set.
	Namespace string `json:"namespace,omitempty"`
	// IngressClassName selects the IngressClass (e.g. "nginx").
	IngressClassName *string `json:"ingressClassName,omitempty"`
	// Rules are the host/path routing rules.
	Rules []DesiredIngressRule `json:"rules,omitempty"`
	// TLS are the TLS termination entries.
	TLS []DesiredIngressTLS `json:"tls,omitempty"`
	// Labels are additional labels merged with the platform-managed labels.
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations are annotations applied to the Ingress (e.g. controller hints).
	Annotations map[string]string `json:"annotations,omitempty"`
}

// DesiredIngressRule is a single host with one or more path routes.
type DesiredIngressRule struct {
	// Host is the request host this rule matches (empty matches all hosts).
	Host string `json:"host,omitempty"`
	// Paths are the path routes under this host.
	Paths []DesiredIngressPath `json:"paths,omitempty"`
}

// DesiredIngressPath routes a path to a backend Service port.
type DesiredIngressPath struct {
	// Path is the HTTP path to match.
	Path string `json:"path"`
	// PathType is "Prefix", "Exact", or "ImplementationSpecific". Defaults to
	// "Prefix" when empty.
	PathType string `json:"pathType,omitempty"`
	// ServiceName is the backend Service name.
	ServiceName string `json:"serviceName"`
	// ServicePort is the backend Service port number.
	ServicePort int32 `json:"servicePort"`
}

// DesiredIngressTLS is a TLS termination entry referencing a Secret.
type DesiredIngressTLS struct {
	// Hosts are the SNI hosts covered by the certificate.
	Hosts []string `json:"hosts,omitempty"`
	// SecretName is the TLS Secret holding the certificate/key.
	SecretName string `json:"secretName,omitempty"`
}

// DesiredPVC represents a PersistentVolumeClaim the agent should reconcile as
// part of a deployment's desired state. It exposes only the fields the platform
// manages; server-populated fields (bound PV, status, capacity) are never sent.
type DesiredPVC struct {
	// Name is the PVC object name. Required.
	Name string `json:"name"`
	// Namespace overrides the deployment namespace when set.
	Namespace string `json:"namespace,omitempty"`
	// AccessModes are the requested access modes (e.g. "ReadWriteOnce").
	// Immutable after creation.
	AccessModes []string `json:"accessModes,omitempty"`
	// StorageClassName selects the StorageClass. Immutable after creation.
	StorageClassName *string `json:"storageClassName,omitempty"`
	// StorageRequest is the requested storage size (e.g. "10Gi"). May only be
	// increased after creation (volume expansion), never decreased.
	StorageRequest string `json:"storageRequest,omitempty"`
	// VolumeMode is "Filesystem" or "Block". Immutable after creation.
	VolumeMode *string `json:"volumeMode,omitempty"`
	// Labels are additional labels merged with the platform-managed labels.
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations are annotations applied to the PVC.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// DesiredConfigMap represents a ConfigMap the agent should reconcile as part of
// a deployment's desired state. It mirrors the reconcilable surface of a
// Kubernetes ConfigMap (data, binaryData, and platform-applied metadata).
type DesiredConfigMap struct {
	// Name is the ConfigMap object name. Required.
	Name string `json:"name"`
	// Data holds UTF-8 string keys/values (corev1.ConfigMap.Data).
	Data map[string]string `json:"data,omitempty"`
	// BinaryData holds binary values (corev1.ConfigMap.BinaryData).
	BinaryData map[string][]byte `json:"binaryData,omitempty"`
	// Labels are additional labels merged with the platform-managed labels.
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations are annotations applied to the ConfigMap.
	Annotations map[string]string `json:"annotations,omitempty"`
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

// DeploymentConditionDTO mirrors a Kubernetes Deployment status condition for
// transport to the control plane.
type DeploymentConditionDTO struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// DeploymentProgressRequest reports rich rollout progress to the control plane.
// It is additive to (and independent of) DeploymentStatusRequest: the terminal
// release status flow via ReportDeploymentStatusWithCreds is unchanged; this
// carries the continuous rollout snapshot (phase, replica counts, conditions,
// rollout percentage) that drives the deployment engine's state machine.
type DeploymentProgressRequest struct {
	// Phase is the rollout state machine phase (see internal/rollout.Phase).
	Phase string `json:"phase"`
	// Revision is the release revision this snapshot describes.
	Revision int `json:"revision"`
	// Image is the container image currently rolled out.
	Image string `json:"image,omitempty"`
	// RolloutPercentage is the completion percentage in [0,100].
	RolloutPercentage int `json:"rolloutPercentage"`
	// Timeout is true when the rollout failed because the progress deadline
	// was exceeded (as opposed to a replica/pod failure).
	Timeout bool `json:"timeout,omitempty"`

	DesiredReplicas     int `json:"desiredReplicas"`
	ReadyReplicas       int `json:"readyReplicas"`
	UpdatedReplicas     int `json:"updatedReplicas"`
	AvailableReplicas   int `json:"availableReplicas"`
	UnavailableReplicas int `json:"unavailableReplicas"`

	ObservedGeneration int64 `json:"observedGeneration"`

	Conditions   []DeploymentConditionDTO `json:"conditions,omitempty"`
	ErrorMessage string                   `json:"errorMessage,omitempty"`
}

// ReportDeploymentProgressWithCreds reports a rollout progress snapshot using
// agent credentials to the dedicated agent progress endpoint. A missing
// endpoint (older control plane) surfaces as an APIError the caller treats as
// non-fatal, preserving backward compatibility.
func (c *Client) ReportDeploymentProgressWithCreds(ctx context.Context, creds AgentCredentials, deploymentID, releaseID string, req DeploymentProgressRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/agent/deployments/%s/releases/%s/progress", c.baseURL, deploymentID, releaseID)
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
