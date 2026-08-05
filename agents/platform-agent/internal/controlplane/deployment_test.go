package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ReportDeploymentProgressWithCreds(t *testing.T) {
	var got DeploymentProgressRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/agent/deployments/dep-1/releases/rel-1/progress", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "cluster-1", r.Header.Get("X-Cluster-ID"))
		assert.Equal(t, "agent-1", r.Header.Get("X-Agent-ID"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	creds := AgentCredentials{ClusterID: "cluster-1", AgentID: "agent-1"}
	err := client.ReportDeploymentProgressWithCreds(context.Background(), creds, "dep-1", "rel-1", DeploymentProgressRequest{
		Phase:             "RollingOut",
		Revision:          2,
		RolloutPercentage: 66,
		DesiredReplicas:   3,
		ReadyReplicas:     2,
	})
	require.NoError(t, err)
	assert.Equal(t, "RollingOut", got.Phase)
	assert.Equal(t, 66, got.RolloutPercentage)
	assert.Equal(t, 2, got.ReadyReplicas)
}

func TestClient_ReportDeploymentProgress_ErrorNonFatal(t *testing.T) {
	// A missing endpoint (older control plane) returns an APIError the caller
	// treats as non-fatal.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	err := client.ReportDeploymentProgressWithCreds(context.Background(),
		AgentCredentials{ClusterID: "c", AgentID: "a"}, "dep-1", "rel-1", DeploymentProgressRequest{Phase: "Healthy"})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}

func TestClient_GetDesiredDeployments(t *testing.T) {
	desiredDeployments := []DesiredDeployment{
		{
			DeploymentID:    "dep-1",
			ReleaseID:       "rel-1",
			ApplicationName: "My App",
			ApplicationSlug: "my-app",
			Image:           "nginx:1.25",
			Replicas:        3,
			Port:            intPtr(8080),
			EnvVars: []EnvVar{
				{Name: "FOO", Value: "bar"},
			},
			ResourceRequests: &ResourceSpec{
				CPU:    "100m",
				Memory: "128Mi",
			},
			Revision: 1,
			Status:   "pending",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Agent API uses /v1/agent/clusters/{clusterId}/desired-state
		assert.Equal(t, "/v1/agent/clusters/cluster-456/desired-state", r.URL.Path)
		// Agent auth uses X-Cluster-ID and X-Agent-ID headers
		assert.Equal(t, "cluster-456", r.Header.Get("X-Cluster-ID"))
		assert.Equal(t, "test-token", r.Header.Get("X-Agent-ID"))
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DesiredStateResponse{Items: desiredDeployments})
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	ctx := context.Background()

	// GetDesiredDeployments is deprecated but wraps GetDesiredState with legacy args
	deployments, err := client.GetDesiredDeployments(ctx, "org-123", "cluster-456", "test-token")

	require.NoError(t, err)
	require.Len(t, deployments, 1)
	assert.Equal(t, "dep-1", deployments[0].DeploymentID)
	assert.Equal(t, "rel-1", deployments[0].ReleaseID)
	assert.Equal(t, "nginx:1.25", deployments[0].Image)
	assert.Equal(t, 3, deployments[0].Replicas)
	assert.Equal(t, 8080, *deployments[0].Port)
	assert.Len(t, deployments[0].EnvVars, 1)
}

func TestClient_GetDesiredDeployments_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	ctx := context.Background()

	_, err := client.GetDesiredDeployments(ctx, "org-123", "cluster-456", "test-token")

	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 500, apiErr.StatusCode)
}

func TestClient_ReportDeploymentStatus_Started(t *testing.T) {
	var receivedRequest DeploymentStatusRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Agent API uses /v1/agent/deployments/{deploymentId}/releases/{releaseId}/status
		assert.Equal(t, "/v1/agent/deployments/dep-456/releases/rel-789/status", r.URL.Path)
		// Agent auth uses X-Cluster-ID and X-Agent-ID headers
		// Legacy ReportDeploymentStatus uses orgID as clusterID and accessToken as agentID
		assert.Equal(t, "org-123", r.Header.Get("X-Cluster-ID"))
		assert.Equal(t, "test-token", r.Header.Get("X-Agent-ID"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		json.NewDecoder(r.Body).Decode(&receivedRequest)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	ctx := context.Background()

	err := client.ReportDeploymentStatus(ctx, "org-123", "dep-456", "rel-789", "test-token", DeploymentStatusRequest{
		Status: "started",
	})

	require.NoError(t, err)
	assert.Equal(t, "started", receivedRequest.Status)
}

func TestClient_ReportDeploymentStatus_Succeeded(t *testing.T) {
	var receivedRequest DeploymentStatusRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedRequest)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	ctx := context.Background()

	readyReplicas := 3
	err := client.ReportDeploymentStatus(ctx, "org-123", "dep-456", "rel-789", "test-token", DeploymentStatusRequest{
		Status:        "succeeded",
		ReadyReplicas: &readyReplicas,
	})

	require.NoError(t, err)
	assert.Equal(t, "succeeded", receivedRequest.Status)
	assert.Equal(t, 3, *receivedRequest.ReadyReplicas)
}

func TestClient_ReportDeploymentStatus_Failed(t *testing.T) {
	var receivedRequest DeploymentStatusRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedRequest)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	ctx := context.Background()

	errorMsg := "ImagePullBackOff"
	err := client.ReportDeploymentStatus(ctx, "org-123", "dep-456", "rel-789", "test-token", DeploymentStatusRequest{
		Status:       "failed",
		ErrorMessage: &errorMsg,
	})

	require.NoError(t, err)
	assert.Equal(t, "failed", receivedRequest.Status)
	assert.Equal(t, "ImagePullBackOff", *receivedRequest.ErrorMessage)
}

func TestClient_ReportDeploymentStatus_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "deployment not found"})
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	ctx := context.Background()

	err := client.ReportDeploymentStatus(ctx, "org-123", "dep-456", "rel-789", "test-token", DeploymentStatusRequest{
		Status: "succeeded",
	})

	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 404, apiErr.StatusCode)
}

func intPtr(i int) *int {
	return &i
}
