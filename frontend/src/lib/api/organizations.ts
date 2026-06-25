import { apiClient } from "./client";
import type {
  Organization,
  OrganizationMember,
  CreateOrganizationRequest,
  UpdateOrganizationRequest,
  InviteMemberRequest,
  Invitation,
  PaginatedResponse,
} from "@/types/api";

export const organizationsApi = {
  async list(params?: { limit?: number; cursor?: string }): Promise<PaginatedResponse<Organization>> {
    return apiClient.get<PaginatedResponse<Organization>>("/v1/organizations", params);
  },

  async get(orgId: string): Promise<Organization> {
    return apiClient.get<Organization>(`/v1/organizations/${orgId}`);
  },

  async getBySlug(slug: string): Promise<Organization> {
    return apiClient.get<Organization>(`/v1/organizations/by-slug/${slug}`);
  },

  async create(data: CreateOrganizationRequest): Promise<Organization> {
    return apiClient.post<Organization>("/v1/organizations", data);
  },

  async update(orgId: string, data: UpdateOrganizationRequest): Promise<Organization> {
    return apiClient.patch<Organization>(`/v1/organizations/${orgId}`, data);
  },

  async delete(orgId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}`);
  },

  // Members
  async listMembers(orgId: string): Promise<PaginatedResponse<OrganizationMember>> {
    return apiClient.get<PaginatedResponse<OrganizationMember>>(`/v1/organizations/${orgId}/members`);
  },

  async updateMemberRole(orgId: string, memberId: string, role: string): Promise<OrganizationMember> {
    return apiClient.patch<OrganizationMember>(`/v1/organizations/${orgId}/members/${memberId}`, { role });
  },

  async removeMember(orgId: string, memberId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/members/${memberId}`);
  },

  // Invitations
  async listInvitations(orgId: string): Promise<PaginatedResponse<Invitation>> {
    return apiClient.get<PaginatedResponse<Invitation>>(`/v1/organizations/${orgId}/invitations`);
  },

  async inviteMember(orgId: string, data: InviteMemberRequest): Promise<Invitation> {
    return apiClient.post<Invitation>(`/v1/organizations/${orgId}/invitations`, data);
  },

  async revokeInvitation(orgId: string, invitationId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/invitations/${invitationId}`);
  },

  async acceptInvitation(token: string): Promise<OrganizationMember> {
    return apiClient.post<OrganizationMember>("/v1/invitations/accept", { token });
  },
};
