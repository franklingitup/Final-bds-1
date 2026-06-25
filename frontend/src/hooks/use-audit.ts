import { useQuery } from "@tanstack/react-query";
import { auditApi } from "@/lib/api";
import type { AuditLogFilter } from "@/types/api";

export function useAuditLogs(
  orgId: string,
  params?: AuditLogFilter & { limit?: number; cursor?: string }
) {
  return useQuery({
    queryKey: ["organizations", orgId, "audit", params],
    queryFn: () => auditApi.list(orgId, params),
    enabled: !!orgId,
  });
}

export function useAuditLog(orgId: string, auditId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "audit", auditId],
    queryFn: () => auditApi.get(orgId, auditId),
    enabled: !!orgId && !!auditId,
  });
}
