import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { applicationsApi } from "@/lib/api";
import type { CreateApplicationRequest, UpdateApplicationRequest } from "@/types/api";
import { toast } from "@/hooks/use-toast";

export function useApplications(orgId: string, projectId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", projectId, "applications"],
    queryFn: () => applicationsApi.list(orgId, projectId),
    enabled: !!orgId && !!projectId,
  });
}

export function useApplication(orgId: string, projectId: string, applicationId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", projectId, "applications", applicationId],
    queryFn: () => applicationsApi.get(orgId, projectId, applicationId),
    enabled: !!orgId && !!projectId && !!applicationId,
  });
}

export function useCreateApplication(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateApplicationRequest) => applicationsApi.create(orgId, projectId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "applications"],
      });
      toast({ title: "Application created successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to create application",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useUpdateApplication(orgId: string, projectId: string, applicationId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: UpdateApplicationRequest) =>
      applicationsApi.update(orgId, projectId, applicationId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "applications"],
      });
      toast({ title: "Application updated successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to update application",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useDeleteApplication(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (applicationId: string) =>
      applicationsApi.delete(orgId, projectId, applicationId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "applications"],
      });
      toast({ title: "Application deleted successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to delete application",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}
