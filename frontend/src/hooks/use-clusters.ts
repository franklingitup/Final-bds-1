import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { clustersApi } from "@/lib/api";
import type { CreateClusterRequest } from "@/types/api";
import { toast } from "@/hooks/use-toast";

export function useClusters(orgId: string, status?: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "clusters", { status }],
    queryFn: () => clustersApi.list(orgId, { status }),
    enabled: !!orgId,
  });
}

export function useCluster(orgId: string, clusterId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "clusters", clusterId],
    queryFn: () => clustersApi.get(orgId, clusterId),
    enabled: !!orgId && !!clusterId,
  });
}

export function useCreateCluster(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateClusterRequest) => clustersApi.create(orgId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "clusters"] });
      toast({ title: "Cluster created successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to create cluster",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useUpdateCluster(orgId: string, clusterId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: Partial<CreateClusterRequest>) =>
      clustersApi.update(orgId, clusterId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "clusters"] });
      toast({ title: "Cluster updated successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to update cluster",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useDeleteCluster(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (clusterId: string) => clustersApi.delete(orgId, clusterId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "clusters"] });
      toast({ title: "Cluster deleted successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to delete cluster",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useGenerateRegistrationToken(orgId: string, clusterId: string) {
  return useMutation({
    mutationFn: (params?: { expiresIn?: string }) =>
      clustersApi.generateToken(orgId, clusterId, params),
    onError: (error) => {
      toast({
        title: "Failed to generate token",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useRevokeRegistrationToken(orgId: string, clusterId: string) {
  return useMutation({
    mutationFn: (tokenId: string) => clustersApi.revokeToken(orgId, clusterId, tokenId),
    onSuccess: () => {
      toast({ title: "Token revoked" });
    },
    onError: (error) => {
      toast({
        title: "Failed to revoke token",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useClusterHeartbeats(orgId: string, clusterId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "clusters", clusterId, "heartbeats"],
    queryFn: () => clustersApi.listHeartbeats(orgId, clusterId, { limit: 10 }),
    enabled: !!orgId && !!clusterId,
    refetchInterval: 30000,
  });
}
