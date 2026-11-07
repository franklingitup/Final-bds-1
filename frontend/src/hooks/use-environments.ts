import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { environmentsApi } from "@/lib/api";
import { ApiClientError } from "@/lib/api/client";
import type { CreateEnvironmentRequest, UpdateEnvironmentRequest } from "@/types/api";
import { toast } from "@/hooks/use-toast";

// ---------------------------------------------------------------------------
// Query keys
// ---------------------------------------------------------------------------

const envKeys = {
  list: (orgId: string, appId: string) =>
    ["organizations", orgId, "applications", appId, "environments"] as const,
  detail: (orgId: string, envId: string) =>
    ["organizations", orgId, "environments", envId] as const,
};

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

export function useEnvironments(orgId: string, appId: string) {
  return useQuery({
    queryKey: envKeys.list(orgId, appId),
    queryFn: () => environmentsApi.list(orgId, appId),
    enabled: !!orgId && !!appId,
    select: (data) =>
      // Return a stable, order_index-sorted array so the UI always renders in
      // the correct order regardless of insertion order from the server.
      [...(data.items ?? [])].sort((a, b) => a.orderIndex - b.orderIndex),
  });
}

export function useCreateEnvironment(orgId: string, appId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateEnvironmentRequest) =>
      environmentsApi.create(orgId, appId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: envKeys.list(orgId, appId) });
      toast({ title: "Environment created" });
    },
    onError: (error: Error) => {
      toast({
        title: "Failed to create environment",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useUpdateEnvironment(orgId: string, environmentId: string, appId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: UpdateEnvironmentRequest) =>
      environmentsApi.update(orgId, environmentId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: envKeys.list(orgId, appId) });
      queryClient.invalidateQueries({ queryKey: envKeys.detail(orgId, environmentId) });
      toast({ title: "Environment updated" });
    },
    onError: (error: Error) => {
      toast({
        title: "Failed to update environment",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

/**
 * Delete an environment.
 *
 * Specifically handles the ON DELETE RESTRICT case: if a pending PromotionRequest
 * references this environment, the DB raises a FK violation (pgcode 23503) which
 * the backend maps to HTTP 400 / code "VALIDATION" / message "referenced resource
 * does not exist". We intercept this and surface a clear message instead of the
 * generic database-level text.
 */
export function useDeleteEnvironment(orgId: string, appId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (environmentId: string) =>
      environmentsApi.delete(orgId, environmentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: envKeys.list(orgId, appId) });
      toast({ title: "Environment deleted" });
    },
    onError: (error: Error) => {
      // Detect the FK-constraint-blocked delete. The backend returns
      //   HTTP 400, code "VALIDATION", message "referenced resource does not exist"
      // when a pending PromotionRequest holds an ON DELETE RESTRICT reference to
      // this environment. Show a clear, specific message instead.
      const isRestrictError =
        ApiClientError.isApiError(error) &&
        error.status === 400 &&
        error.message.toLowerCase().includes("referenced resource");

      toast({
        title: "Cannot delete environment",
        description: isRestrictError
          ? "This environment has pending promotions referencing it. Cancel all pending promotions for this environment before deleting it."
          : error.message,
        variant: "destructive",
      });
    },
  });
}
