import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { buildsApi, type CreateBuildRequest } from "@/lib/api/builds";
import { toast } from "@/hooks/use-toast";

export function useRepositories(orgId: string, projectId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", projectId, "repositories"],
    queryFn: () => buildsApi.listRepositories(orgId, projectId),
    enabled: !!orgId && !!projectId,
  });
}

export function useCreateBuild(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateBuildRequest) => buildsApi.createBuild(orgId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "builds"] });
      toast({ title: "Build triggered", description: "Your build has been queued." });
    },
    onError: (error: Error) => {
      toast({
        title: "Failed to trigger build",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}
