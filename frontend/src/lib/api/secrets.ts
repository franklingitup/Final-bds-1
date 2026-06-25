import { apiClient } from "./client";
import type { Secret, CreateSecretRequest, UpdateSecretRequest, PaginatedResponse } from "@/types/api";

export const secretsApi = {
  async list(
    orgId: string,
    projectId: string,
    params?: { limit?: number; cursor?: string }
  ): Promise<PaginatedResponse<Secret>> {
    return apiClient.get<PaginatedResponse<Secret>>(
      `/v1/organizations/${orgId}/projects/${projectId}/secrets`,
      params
    );
  },

  async get(orgId: string, projectId: string, secretId: string): Promise<Secret> {
    return apiClient.get<Secret>(`/v1/organizations/${orgId}/projects/${projectId}/secrets/${secretId}`);
  },

  async create(orgId: string, projectId: string, data: CreateSecretRequest): Promise<Secret> {
    return apiClient.post<Secret>(`/v1/organizations/${orgId}/projects/${projectId}/secrets`, data);
  },

  async update(
    orgId: string,
    projectId: string,
    secretId: string,
    data: UpdateSecretRequest
  ): Promise<Secret> {
    return apiClient.patch<Secret>(
      `/v1/organizations/${orgId}/projects/${projectId}/secrets/${secretId}`,
      data
    );
  },

  async delete(orgId: string, projectId: string, secretId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/projects/${projectId}/secrets/${secretId}`);
  },
};
