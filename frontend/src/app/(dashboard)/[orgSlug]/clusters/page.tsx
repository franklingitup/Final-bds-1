"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { Plus, Server } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { ClusterStatusBadge } from "@/components/shared/status-badge";
import { useClusters } from "@/hooks/use-clusters";
import { useOrganization } from "@/providers/organization-provider";
import { formatRelativeTime } from "@/lib/utils";
import { CreateClusterDialog } from "@/components/clusters/create-cluster-dialog";
import { ClusterCard } from "@/components/clusters/cluster-card";

export default function ClustersPage() {
  const params = useParams();
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const { data, isLoading, error, refetch } = useClusters(organization?.id || "");
  const [createDialogOpen, setCreateDialogOpen] = React.useState(false);

  if (isLoading) {
    return <LoadingState message="Loading clusters..." />;
  }

  if (error) {
    return <ErrorState onRetry={() => refetch()} />;
  }

  const clusters = data?.items || [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Clusters"
        description="Manage your Kubernetes clusters"
        action={
          <Button onClick={() => setCreateDialogOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Add Cluster
          </Button>
        }
      />

      {clusters.length === 0 ? (
        <EmptyState
          icon={<Server className="h-6 w-6" />}
          title="No clusters yet"
          description="Connect your first Kubernetes cluster to start deploying"
          action={
            <Button onClick={() => setCreateDialogOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              Add Cluster
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {clusters.map((cluster) => (
            <ClusterCard
              key={cluster.id}
              cluster={cluster}
              orgSlug={orgSlug}
            />
          ))}
        </div>
      )}

      <CreateClusterDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        orgId={organization?.id || ""}
        orgSlug={orgSlug}
      />
    </div>
  );
}
