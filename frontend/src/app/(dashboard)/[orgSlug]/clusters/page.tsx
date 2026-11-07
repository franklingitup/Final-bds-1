"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Plus, Server, Cloud, ChevronDown } from "lucide-react";
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
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button>
                <Plus className="mr-2 h-4 w-4" />
                Add Cluster
                <ChevronDown className="ml-2 h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setCreateDialogOpen(true)}>
                <Server className="mr-2 h-4 w-4" />
                Register Existing Cluster
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link href={`/${orgSlug}/clusters/provision`}>
                  <Cloud className="mr-2 h-4 w-4" />
                  Provision New Cluster
                </Link>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        }
      />

      {clusters.length === 0 ? (
        <EmptyState
          icon={<Server className="h-6 w-6" />}
          title="No clusters yet"
          description="Connect your first Kubernetes cluster to start deploying"
          action={
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setCreateDialogOpen(true)}>
                <Server className="mr-2 h-4 w-4" />
                Register Existing
              </Button>
              <Link href={`/${orgSlug}/clusters/provision`}>
                <Button>
                  <Cloud className="mr-2 h-4 w-4" />
                  Provision New
                </Button>
              </Link>
            </div>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {clusters.map((cluster) => (
            <ClusterCard
              key={cluster.id}
              cluster={cluster}
              orgId={organization?.id || ""}
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
