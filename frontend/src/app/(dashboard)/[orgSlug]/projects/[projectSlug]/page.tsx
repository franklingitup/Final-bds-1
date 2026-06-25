"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { Plus, Box, Key, Rocket } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { useProjectBySlug } from "@/hooks/use-projects";
import { useApplications } from "@/hooks/use-applications";
import { useSecrets } from "@/hooks/use-secrets";
import { useDeployments } from "@/hooks/use-deployments";
import { useOrganization } from "@/providers/organization-provider";
import { formatRelativeTime } from "@/lib/utils";
import { CreateApplicationDialog } from "@/components/applications/create-application-dialog";
import { CreateSecretDialog } from "@/components/secrets/create-secret-dialog";
import { ApplicationCard } from "@/components/applications/application-card";
import { SecretsList } from "@/components/secrets/secrets-list";
import { DeploymentsList } from "@/components/deployments/deployments-list";

export default function ProjectDetailPage() {
  const params = useParams();
  const projectSlug = params.projectSlug as string;
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";

  const { data: project, isLoading: isProjectLoading, error: projectError, refetch: refetchProject } = 
    useProjectBySlug(orgId, projectSlug);
  
  const projectId = project?.id || "";
  
  const { data: apps, isLoading: isAppsLoading } = useApplications(orgId, projectId);
  const { data: secrets, isLoading: isSecretsLoading } = useSecrets(orgId, projectId);
  const { data: deployments, isLoading: isDeploymentsLoading } = useDeployments(orgId, { applicationId: projectId });

  const [createAppOpen, setCreateAppOpen] = React.useState(false);
  const [createSecretOpen, setCreateSecretOpen] = React.useState(false);

  if (isProjectLoading) {
    return <LoadingState message="Loading project..." />;
  }

  if (projectError) {
    return <ErrorState onRetry={() => refetchProject()} />;
  }

  if (!project) {
    return <ErrorState title="Project not found" message="The project you're looking for doesn't exist." />;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={project.name}
        description={project.description || "No description"}
      />

      <Tabs defaultValue="applications" className="space-y-6">
        <TabsList>
          <TabsTrigger value="applications" className="gap-2">
            <Box className="h-4 w-4" />
            Applications
          </TabsTrigger>
          <TabsTrigger value="secrets" className="gap-2">
            <Key className="h-4 w-4" />
            Secrets
          </TabsTrigger>
          <TabsTrigger value="deployments" className="gap-2">
            <Rocket className="h-4 w-4" />
            Deployments
          </TabsTrigger>
        </TabsList>

        <TabsContent value="applications" className="space-y-4">
          <div className="flex justify-end">
            <Button onClick={() => setCreateAppOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              New Application
            </Button>
          </div>

          {isAppsLoading ? (
            <LoadingState message="Loading applications..." />
          ) : apps?.items.length === 0 ? (
            <EmptyState
              icon={<Box className="h-6 w-6" />}
              title="No applications yet"
              description="Create an application to start deploying"
              action={
                <Button onClick={() => setCreateAppOpen(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  Create Application
                </Button>
              }
            />
          ) : (
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              {apps?.items.map((app) => (
                <ApplicationCard
                  key={app.id}
                  application={app}
                  orgSlug={orgSlug}
                  projectSlug={projectSlug}
                />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="secrets" className="space-y-4">
          <div className="flex justify-end">
            <Button onClick={() => setCreateSecretOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              New Secret
            </Button>
          </div>

          {isSecretsLoading ? (
            <LoadingState message="Loading secrets..." />
          ) : secrets?.items.length === 0 ? (
            <EmptyState
              icon={<Key className="h-6 w-6" />}
              title="No secrets yet"
              description="Create secrets to store sensitive configuration"
              action={
                <Button onClick={() => setCreateSecretOpen(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  Create Secret
                </Button>
              }
            />
          ) : (
            <SecretsList
              secrets={secrets?.items || []}
              orgId={orgId}
              projectId={projectId}
            />
          )}
        </TabsContent>

        <TabsContent value="deployments" className="space-y-4">
          {isDeploymentsLoading ? (
            <LoadingState message="Loading deployments..." />
          ) : deployments?.items.length === 0 ? (
            <EmptyState
              icon={<Rocket className="h-6 w-6" />}
              title="No deployments yet"
              description="Deploy an application to get started"
            />
          ) : (
            <DeploymentsList
              deployments={deployments?.items || []}
              orgSlug={orgSlug}
            />
          )}
        </TabsContent>
      </Tabs>

      <CreateApplicationDialog
        open={createAppOpen}
        onOpenChange={setCreateAppOpen}
        orgId={orgId}
        projectId={projectId}
      />

      <CreateSecretDialog
        open={createSecretOpen}
        onOpenChange={setCreateSecretOpen}
        orgId={orgId}
        projectId={projectId}
      />
    </div>
  );
}
