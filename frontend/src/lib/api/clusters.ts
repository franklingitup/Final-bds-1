import { apiClient } from "./client";
import type {
  Cluster,
  CreateClusterRequest,
  RegistrationToken,
  Heartbeat,
  PaginatedResponse,
} from "@/types/api";

export const clustersApi = {
  async list(
    orgId: string,
    params?: { status?: string; limit?: number; cursor?: string }
  ): Promise<PaginatedResponse<Cluster>> {
    return apiClient.get<PaginatedResponse<Cluster>>(`/v1/organizations/${orgId}/clusters`, params);
  },

  async get(orgId: string, clusterId: string): Promise<Cluster> {
    return apiClient.get<Cluster>(`/v1/organizations/${orgId}/clusters/${clusterId}`);
  },

  async create(orgId: string, data: CreateClusterRequest): Promise<Cluster> {
    return apiClient.post<Cluster>(`/v1/organizations/${orgId}/clusters`, data);
  },

  async update(
    orgId: string,
    clusterId: string,
    data: Partial<CreateClusterRequest>
  ): Promise<Cluster> {
    return apiClient.patch<Cluster>(`/v1/organizations/${orgId}/clusters/${clusterId}`, data);
  },

  async delete(orgId: string, clusterId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/clusters/${clusterId}`);
  },

  // Registration tokens
  async generateToken(
    orgId: string,
    clusterId: string,
    params?: { expiresIn?: string }
  ): Promise<RegistrationToken> {
    return apiClient.post<RegistrationToken>(
      `/v1/organizations/${orgId}/clusters/${clusterId}/tokens`,
      params
    );
  },

  async revokeToken(orgId: string, clusterId: string, tokenId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/clusters/${clusterId}/tokens/${tokenId}`);
  },

  // Heartbeats
  async listHeartbeats(
    orgId: string,
    clusterId: string,
    params?: { limit?: number }
  ): Promise<PaginatedResponse<Heartbeat>> {
    return apiClient.get<PaginatedResponse<Heartbeat>>(
      `/v1/organizations/${orgId}/clusters/${clusterId}/heartbeats`,
      params
    );
  },
};

export function getInstallCommand(token: string, controlPlaneUrl: string): string {
  return `helm install platform-agent oci://registry.bdsplatform.io/charts/platform-agent \\
  --namespace bdsplatform-system \\
  --create-namespace \\
  --set token=${token} \\
  --set controlPlaneUrl=${controlPlaneUrl} \\
  --set reconciler.enabled=true \\
  --set secretsSyncer.enabled=true`;
}
