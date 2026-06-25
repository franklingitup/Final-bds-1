import { apiClient } from "./client";
import type {
  Project,
  ProjectMember,
  CreateProjectRequest,
  UpdateProjectRequest,
  PaginatedResponse,
} from "@/types/api";

export const projectsApi = {
  async list(orgId: string, params?: { limit?: number; cursor?: string }): Promise<PaginatedResponse<Project>> {
    return apiClient.get<PaginatedResponse<Project>>(`/v1/organizations/${orgId}/projects`, params);
  },

  async get(orgId: string, projectId: string): Promise<Project> {
    return apiClient.get<Project>(`/v1/organizations/${orgId}/projects/${projectId}`);
  },

  async getBySlug(orgId: string, slug: string): Promise<Project> {
    return apiClient.get<Project>(`/v1/organizations/${orgId}/projects/by-slug/${slug}`);
  },

  async create(orgId: string, data: CreateProjectRequest): Promise<Project> {
    return apiClient.post<Project>(`/v1/organizations/${orgId}/projects`, data);
  },

  async update(orgId: string, projectId: string, data: UpdateProjectRequest): Promise<Project> {
    return apiClient.patch<Project>(`/v1/organizations/${orgId}/projects/${projectId}`, data);
  },

  async delete(orgId: string, projectId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/projects/${projectId}`);
  },

  // Members
  async listMembers(orgId: string, projectId: string): Promise<PaginatedResponse<ProjectMember>> {
    return apiClient.get<PaginatedResponse<ProjectMember>>(`/v1/organizations/${orgId}/projects/${projectId}/members`);
  },

  async addMember(orgId: string, projectId: string, data: { userId: string; role: string }): Promise<ProjectMember> {
    return apiClient.post<ProjectMember>(`/v1/organizations/${orgId}/projects/${projectId}/members`, data);
  },

  async updateMemberRole(
    orgId: string,
    projectId: string,
    memberId: string,
    role: string
  ): Promise<ProjectMember> {
    return apiClient.patch<ProjectMember>(
      `/v1/organizations/${orgId}/projects/${projectId}/members/${memberId}`,
      { role }
    );
  },

  async removeMember(orgId: string, projectId: string, memberId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/projects/${projectId}/members/${memberId}`);
  },
};
