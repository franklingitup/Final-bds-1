"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { Plus, Rocket } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { DeploymentsList } from "@/components/deployments/deployments-list";
import { useDeployments } from "@/hooks/use-deployments";
import { useOrganization } from "@/providers/organization-provider";
import { CreateDeploymentDialog } from "@/components/deployments/create-deployment-dialog";

export default function DeploymentsPage() {
  const params = useParams();
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const { data, isLoading, error, refetch } = useDeployments(organization?.id || "");
  const [createDialogOpen, setCreateDialogOpen] = React.useState(false);

  if (isLoading) {
    return <LoadingState message="Loading deployments..." />;
  }

  if (error) {
    return <ErrorState onRetry={() => refetch()} />;
  }

  const deployments = data?.items || [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Deployments"
        description="Manage your application deployments"
        action={
          <Button onClick={() => setCreateDialogOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            New Deployment
          </Button>
        }
      />

      {deployments.length === 0 ? (
        <EmptyState
          icon={<Rocket className="h-6 w-6" />}
          title="No deployments yet"
          description="Create a deployment to run your applications on a cluster"
          action={
            <Button onClick={() => setCreateDialogOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              Create Deployment
            </Button>
          }
        />
      ) : (
        <DeploymentsList deployments={deployments} orgSlug={orgSlug} />
      )}

      <CreateDeploymentDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        orgId={organization?.id || ""}
      />
    </div>
  );
}
