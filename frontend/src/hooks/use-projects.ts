import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { projectsApi } from "@/lib/api";
import type { CreateProjectRequest, UpdateProjectRequest } from "@/types/api";
import { toast } from "@/hooks/use-toast";

export function useProjects(orgId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects"],
    queryFn: () => projectsApi.list(orgId),
    enabled: !!orgId,
  });
}

export function useProject(orgId: string, projectId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", projectId],
    queryFn: () => projectsApi.get(orgId, projectId),
    enabled: !!orgId && !!projectId,
  });
}

export function useProjectBySlug(orgId: string, slug: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", "by-slug", slug],
    queryFn: () => projectsApi.getBySlug(orgId, slug),
    enabled: !!orgId && !!slug,
  });
}

export function useCreateProject(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateProjectRequest) => projectsApi.create(orgId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "projects"] });
      toast({ title: "Project created successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to create project",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useUpdateProject(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: UpdateProjectRequest) => projectsApi.update(orgId, projectId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "projects"] });
      toast({ title: "Project updated successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to update project",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useDeleteProject(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (projectId: string) => projectsApi.delete(orgId, projectId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["organizations", orgId, "projects"] });
      toast({ title: "Project deleted successfully" });
    },
    onError: (error) => {
      toast({
        title: "Failed to delete project",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useProjectMembers(orgId: string, projectId: string) {
  return useQuery({
    queryKey: ["organizations", orgId, "projects", projectId, "members"],
    queryFn: () => projectsApi.listMembers(orgId, projectId),
    enabled: !!orgId && !!projectId,
  });
}

export function useAddProjectMember(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: { userId: string; role: string }) =>
      projectsApi.addMember(orgId, projectId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "members"],
      });
      toast({ title: "Member added" });
    },
    onError: (error) => {
      toast({
        title: "Failed to add member",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useUpdateProjectMemberRole(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ memberId, role }: { memberId: string; role: string }) =>
      projectsApi.updateMemberRole(orgId, projectId, memberId, role),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "members"],
      });
      toast({ title: "Member role updated" });
    },
    onError: (error) => {
      toast({
        title: "Failed to update member role",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}

export function useRemoveProjectMember(orgId: string, projectId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (memberId: string) => projectsApi.removeMember(orgId, projectId, memberId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["organizations", orgId, "projects", projectId, "members"],
      });
      toast({ title: "Member removed" });
    },
    onError: (error) => {
      toast({
        title: "Failed to remove member",
        description: error.message,
        variant: "destructive",
      });
    },
  });
}
