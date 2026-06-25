import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { deploymentsApi } from "@/lib/api";
import type { CreateDeploymentRequest, UpdateDeploymentRequest, RollbackRequest } from "@/types/api";
import { toast } from "@/hooks/use-toast";

export function useDeployments(
  orgId: string,
  params?: { applicationId?: string; clusterId?: string }
) {
  return useQuery({
    queryKey: ["organizations", orgId, "deployments", params],
    queryFn: () => deploymentsApi.list(orgId, params),
    enabled: !!orgId,
  });
}

export function useDeployment(orgId: string, deploymentId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "deployments", deploymentId],
    queryFn: () => deploymentsApi.get(orgId, deploymentId),
    enabled: !!orgId && !!deploymentId,
    refetchInterval: 5000,
  });
}

export function useCreateDeployment(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateDeploymentRequest) => deploymentsApi.create(orgId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "deployments"] });
      toast({ title: "Deployment created successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to create deployment",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useUpdateDeployment(orgId: string, deploymentId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: UpdateDeploymentRequest) =>
      deploymentsApi.update(orgId, deploymentId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "deployments"] });
      toast({ title: "Deployment updated successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to update deployment",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useDeleteDeployment(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (deploymentId: string) => deploymentsApi.delete(orgId, deploymentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "deployments"] });
      toast({ title: "Deployment deleted successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to delete deployment",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useReleases(orgId: string, deploymentId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "deployments", deploymentId, "releases"],
    queryFn: () => deploymentsApi.listReleases(orgId, deploymentId),
    enabled: !!orgId && !!deploymentId,
  });
}

export function useRelease(orgId: string, deploymentId: string, releaseId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "deployments", deploymentId, "releases", releaseId],
    queryFn: () => deploymentsApi.getRelease(orgId, deploymentId, releaseId),
    enabled: !!orgId && !!deploymentId && !!releaseId,
  });
}

export function useRollback(orgId: string, deploymentId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data?: RollbackRequest) => deploymentsApi.rollback(orgId, deploymentId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "deployments"] });
      toast({ title: "Rollback initiated" });
    },
    onError: (error) => {
      toast({
        title: "Failed to rollback",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}
