import { apiClient } from "./client";
import type {
  Application,
  CreateApplicationRequest,
  UpdateApplicationRequest,
  PaginatedResponse,
} from "@/types/api";

// API-CRIT-08: Backend uses org-scoped paths for get/update/delete
export const applicationsApi = {
  // List uses project-scoped path
  async list(
    orgId: string,
    projectId: string,
    params?: { limit?: number; cursor?: string }
  ): Promise<PaginatedResponse<Application>> {
    return apiClient.get<PaginatedResponse<Application>>(
      `/v1/organizations/${orgId}/projects/${projectId}/applications`,
      params
    );
  },

  // Get uses org-scoped path (backend: /organizations/:orgId/applications/:appId)
  async get(orgId: string, applicationId: string): Promise<Application> {
    return apiClient.get<Application>(
      `/v1/organizations/${orgId}/applications/${applicationId}`
    );
  },

  // Create uses project-scoped path
  async create(orgId: string, projectId: string, data: CreateApplicationRequest): Promise<Application> {
    return apiClient.post<Application>(
      `/v1/organizations/${orgId}/projects/${projectId}/applications`,
      data
    );
  },

  // Update uses org-scoped path
  async update(
    orgId: string,
    applicationId: string,
    data: UpdateApplicationRequest
  ): Promise<Application> {
    return apiClient.patch<Application>(
      `/v1/organizations/${orgId}/applications/${applicationId}`,
      data
    );
  },

  // Delete uses org-scoped path
  async delete(orgId: string, applicationId: string): Promise<void> {
    return apiClient.delete(
      `/v1/organizations/${orgId}/applications/${applicationId}`
    );
  },
};
