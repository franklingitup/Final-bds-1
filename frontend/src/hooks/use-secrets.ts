import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { secretsApi } from "@/lib/api";
import type { CreateSecretRequest, UpdateSecretRequest } from "@/types/api";
import { toast } from "@/hooks/use-toast";

export function useSecrets(orgId: string, projectId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", projectId, "secrets"],
    queryFn: () => secretsApi.list(orgId, projectId),
    enabled: !!orgId && !!projectId,
  });
}

export function useSecret(orgId: string, projectId: string, secretId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", projectId, "secrets", secretId],
    queryFn: () => secretsApi.get(orgId, projectId, secretId),
    enabled: !!orgId && !!projectId && !!secretId,
  });
}

export function useCreateSecret(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateSecretRequest) => secretsApi.create(orgId, projectId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "secrets"],
      });
      toast({ title: "Secret created successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to create secret",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useUpdateSecret(orgId: string, projectId: string, secretId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: UpdateSecretRequest) => secretsApi.update(orgId, projectId, secretId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "secrets"],
      });
      toast({ title: "Secret updated successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to update secret",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useDeleteSecret(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (secretId: string) => secretsApi.delete(orgId, projectId, secretId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "secrets"],
      });
      toast({ title: "Secret deleted successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to delete secret",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}
