package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Register(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "successful registration",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/v1/agent/register" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}

				var req RegisterRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if req.Token != "test-token" {
					t.Errorf("unexpected token: %s", req.Token)
				}

				resp := RegisterResponse{
					ID:                "cluster-123",
					OrganizationID:    "org-123",
					Name:              "Test Cluster",
					Slug:              "test-cluster",
					Status:            "connected",
					KubernetesVersion: req.KubernetesVersion,
					AgentID:           req.AgentID,
					RegisteredAt:      time.Now(),
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			},
			wantErr: false,
		},
		{
			name: "invalid token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid or expired token"})
			},
			wantErr:    true,
			wantErrMsg: "control plane error (status 401): invalid or expired token",
		},
		{
			name: "token already used",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "registration token already used"})
			},
			wantErr:    true,
			wantErrMsg: "control plane error (status 409): registration token already used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewClient(server.URL, 5*time.Second)
			resp, err := client.Register(context.Background(), RegisterRequest{
				Token:             "test-token",
				AgentID:           "agent-123",
				KubernetesVersion: "1.28.5",
				NodeCount:         3,
				CloudProvider:     "aws",
				Region:            "us-west-2",
			})

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if err.Error() != tt.wantErrMsg {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErrMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.ID != "cluster-123" {
				t.Errorf("ID = %q, want %q", resp.ID, "cluster-123")
			}
			if resp.OrganizationID != "org-123" {
				t.Errorf("OrganizationID = %q, want %q", resp.OrganizationID, "org-123")
			}
		})
	}
}

func TestClient_Heartbeat(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "successful heartbeat",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				// Agent API uses /v1/agent/clusters/{clusterId}/heartbeat
				if r.URL.Path != "/v1/agent/clusters/cluster-123/heartbeat" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				// Agent auth uses X-Cluster-ID and X-Agent-ID headers
				if r.Header.Get("X-Cluster-ID") != "cluster-123" {
					t.Errorf("unexpected X-Cluster-ID: %s", r.Header.Get("X-Cluster-ID"))
				}
				if r.Header.Get("X-Agent-ID") != "agent-123" {
					t.Errorf("unexpected X-Agent-ID: %s", r.Header.Get("X-Agent-ID"))
				}

				var req HeartbeatRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if req.AgentID != "agent-123" {
					t.Errorf("unexpected agent ID in body: %s", req.AgentID)
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(HeartbeatResponse{Status: "ok"})
			},
			wantErr: false,
		},
		{
			name: "agent ID mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "agent ID mismatch"})
			},
			wantErr:    true,
			wantErrMsg: "control plane error (status 403): agent ID mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewClient(server.URL, 5*time.Second)
			err := client.Heartbeat(context.Background(), "org-123", "cluster-123", HeartbeatRequest{
				AgentID:           "agent-123",
				KubernetesVersion: "1.28.5",
				NodeCount:         3,
				APIServerHealthy:  true,
			})

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if err.Error() != tt.wantErrMsg {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErrMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAPIError(t *testing.T) {
	tests := []struct {
		name       string
		err        *APIError
		wantUnauth bool
		wantConfl  bool
		wantForb   bool
	}{
		{
			name:       "unauthorized",
			err:        &APIError{StatusCode: 401, Message: "invalid token"},
			wantUnauth: true,
		},
		{
			name:      "conflict",
			err:       &APIError{StatusCode: 409, Message: "token used"},
			wantConfl: true,
		},
		{
			name:     "forbidden",
			err:      &APIError{StatusCode: 403, Message: "agent mismatch"},
			wantForb: true,
		},
		{
			name: "other",
			err:  &APIError{StatusCode: 500, Message: "internal error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.IsUnauthorized(); got != tt.wantUnauth {
				t.Errorf("IsUnauthorized() = %v, want %v", got, tt.wantUnauth)
			}
			if got := tt.err.IsConflict(); got != tt.wantConfl {
				t.Errorf("IsConflict() = %v, want %v", got, tt.wantConfl)
			}
			if got := tt.err.IsForbidden(); got != tt.wantForb {
				t.Errorf("IsForbidden() = %v, want %v", got, tt.wantForb)
			}
		})
	}
}
