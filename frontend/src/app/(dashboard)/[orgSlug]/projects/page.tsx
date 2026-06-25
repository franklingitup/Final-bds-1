"use client";

import * as React from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { Plus, FolderKanban } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { useProjects } from "@/hooks/use-projects";
import { useOrganization } from "@/providers/organization-provider";
import { formatRelativeTime } from "@/lib/utils";
import { CreateProjectDialog } from "@/components/projects/create-project-dialog";

export default function ProjectsPage() {
  const params = useParams();
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const { data, isLoading, error, refetch } = useProjects(organization?.id || "");
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
            <Link key={project.id} href={`/${orgSlug}/projects/${project.slug}`}>
              <Card className="cursor-pointer transition-colors hover:bg-accent/50">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
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
              </Card>
            </Link>
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
