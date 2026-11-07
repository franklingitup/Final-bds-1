"use client";

import * as React from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { FolderKanban, MoreHorizontal, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useDeleteProject, useProjects } from "@/hooks/use-projects";
import { useOrganization } from "@/providers/organization-provider";
import { formatRelativeTime } from "@/lib/utils";
import { CreateProjectDialog } from "@/components/projects/create-project-dialog";

export default function ProjectsPage() {
  const params = useParams();
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";
  const { data, isLoading, error, refetch } = useProjects(orgId);
  const deleteProject = useDeleteProject(orgId);
  const [createDialogOpen, setCreateDialogOpen] = React.useState(false);

  if (isLoading) {
    return <LoadingState message="Loading projects..." />;
  }

  if (error) {
    return <ErrorState onRetry={() => refetch()} />;
  }

  const projects = data?.items || [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Projects"
        description="Manage your applications and deployments"
        action={
          <Button onClick={() => setCreateDialogOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            New Project
          </Button>
        }
      />

      {projects.length === 0 ? (
        <EmptyState
          icon={<FolderKanban className="h-6 w-6" />}
          title="No projects yet"
          description="Create your first project to start deploying applications"
          action={
            <Button onClick={() => setCreateDialogOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              Create Project
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {projects.map((project) => (
            <Card key={project.id} className="relative transition-colors hover:bg-accent/50">
              <Link href={`/${orgSlug}/projects/${project.slug}`} className="block">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 pr-10">
                    <FolderKanban className="h-5 w-5 text-muted-foreground" />
                    {project.name}
                  </CardTitle>
                  {project.description && (
                    <CardDescription className="line-clamp-2">
                      {project.description}
                    </CardDescription>
                  )}
                </CardHeader>
                <CardContent>
                  <p className="text-sm text-muted-foreground">
                    Created {formatRelativeTime(project.createdAt)}
                  </p>
                </CardContent>
              </Link>
              <div className="absolute right-3 top-3">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Actions for ${project.name}`}
                      disabled={deleteProject.isPending}
                    >
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <ConfirmDialog
                      trigger={
                        <DropdownMenuItem
                          className="text-destructive focus:text-destructive"
                          onSelect={(event) => event.preventDefault()}
                          disabled={deleteProject.isPending}
                        >
                          <Trash2 className="h-4 w-4" />
                          {deleteProject.isPending ? "Deleting..." : "Delete"}
                        </DropdownMenuItem>
                      }
                      title="Delete Project"
                      description={`Delete ${project.name}? This will permanently delete the project and its associated data.`}
                      confirmLabel="Delete"
                      variant="destructive"
                      isLoading={deleteProject.isPending}
                      onConfirm={() => deleteProject.mutateAsync(project.id)}
                    />
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </Card>
          ))}
        </div>
      )}

      <CreateProjectDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        orgId={organization?.id || ""}
        orgSlug={orgSlug}
      />
    </div>
  );
}
