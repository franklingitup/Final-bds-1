import { apiClient } from "./client";
import type {
  Deployment,
  CreateDeploymentRequest,
  UpdateDeploymentRequest,
  Release,
  RollbackRequest,
  PaginatedResponse,
} from "@/types/api";

export const deploymentsApi = {
  async list(
    orgId: string,
    params?: { applicationId?: string; clusterId?: string; limit?: number; cursor?: string }
  ): Promise<PaginatedResponse<Deployment>> {
    return apiClient.get<PaginatedResponse<Deployment>>(`/v1/organizations/${orgId}/deployments`, params);
  },

  async get(orgId: string, deploymentId: string): Promise<Deployment> {
    return apiClient.get<Deployment>(`/v1/organizations/${orgId}/deployments/${deploymentId}`);
  },

  async create(orgId: string, data: CreateDeploymentRequest): Promise<Deployment> {
    return apiClient.post<Deployment>(`/v1/organizations/${orgId}/deployments`, data);
  },

  async update(orgId: string, deploymentId: string, data: UpdateDeploymentRequest): Promise<Deployment> {
    return apiClient.patch<Deployment>(`/v1/organizations/${orgId}/deployments/${deploymentId}`, data);
  },

  async delete(orgId: string, deploymentId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/deployments/${deploymentId}`);
  },

  // Releases
  async listReleases(
    orgId: string,
    deploymentId: string,
    params?: { limit?: number; cursor?: string }
  ): Promise<PaginatedResponse<Release>> {
    return apiClient.get<PaginatedResponse<Release>>(
      `/v1/organizations/${orgId}/deployments/${deploymentId}/releases`,
      params
    );
  },

  async getRelease(orgId: string, deploymentId: string, releaseId: string): Promise<Release> {
    return apiClient.get<Release>(
      `/v1/organizations/${orgId}/deployments/${deploymentId}/releases/${releaseId}`
    );
  },

  async rollback(orgId: string, deploymentId: string, data?: RollbackRequest): Promise<Release> {
    return apiClient.post<Release>(
      `/v1/organizations/${orgId}/deployments/${deploymentId}/rollback`,
      data
    );
  },
};
