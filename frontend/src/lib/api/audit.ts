import { apiClient } from "./client";
import type { AuditLog, AuditLogFilter, PaginatedResponse } from "@/types/api";

// API-CRIT-07: Backend uses /audit-logs, not /audit
export const auditApi = {
  async list(
    orgId: string,
    params?: AuditLogFilter & { limit?: number; cursor?: string }
  ): Promise<PaginatedResponse<AuditLog>> {
    return apiClient.get<PaginatedResponse<AuditLog>>(`/v1/organizations/${orgId}/audit-logs`, params);
  },

  async get(orgId: string, eventId: string): Promise<AuditLog> {
    return apiClient.get<AuditLog>(`/v1/organizations/${orgId}/audit-logs/${eventId}`);
  },
};
