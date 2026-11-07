"use client";

import Link from "next/link";
import { Cpu, HardDrive, MoreHorizontal, Server, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { ClusterStatusBadge } from "@/components/shared/status-badge";
import { useDeleteCluster } from "@/hooks/use-clusters";
import type { Cluster } from "@/types/api";
import { formatRelativeTime } from "@/lib/utils";

interface ClusterCardProps {
  cluster: Cluster;
  orgId: string;
  orgSlug: string;
}

export function ClusterCard({ cluster, orgId, orgSlug }: ClusterCardProps) {
  const deleteCluster = useDeleteCluster(orgId);

  return (
    <Card className="relative transition-colors hover:bg-accent/50">
      <Link href={`/${orgSlug}/clusters/${cluster.id}`} className="block">
        <CardHeader>
          <div className="flex items-center justify-between gap-2 pr-10">
            <CardTitle className="flex items-center gap-2">
              <Server className="h-5 w-5 text-muted-foreground" />
              {cluster.name}
            </CardTitle>
            <ClusterStatusBadge status={cluster.status} />
          </div>
          {cluster.description && (
            <CardDescription className="line-clamp-2">
              {cluster.description}
            </CardDescription>
          )}
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-4 text-sm text-muted-foreground">
            {cluster.kubernetesVersion && (
              <span className="flex items-center gap-1">
                <Cpu className="h-4 w-4" />
                {cluster.kubernetesVersion}
              </span>
            )}
            {cluster.nodeCount && (
              <span className="flex items-center gap-1">
                <HardDrive className="h-4 w-4" />
                {cluster.nodeCount} nodes
              </span>
            )}
            {cluster.cloudProvider && (
              <span>{cluster.cloudProvider}</span>
            )}
          </div>
          {cluster.lastHeartbeatAt && (
            <p className="mt-2 text-xs text-muted-foreground">
              Last seen {formatRelativeTime(cluster.lastHeartbeatAt)}
            </p>
          )}
        </CardContent>
      </Link>
      <div className="absolute right-3 top-3">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Actions for ${cluster.name}`}
              disabled={deleteCluster.isPending}
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
                  disabled={deleteCluster.isPending}
                >
                  <Trash2 className="h-4 w-4" />
                  {deleteCluster.isPending ? "Deleting..." : "Delete"}
                </DropdownMenuItem>
              }
              title="Delete Cluster"
              description={`Delete ${cluster.name}? This may remove infrastructure and deployments tied to this cluster. This action cannot be undone.`}
              confirmLabel="Delete"
              variant="destructive"
              isLoading={deleteCluster.isPending}
              onConfirm={() => deleteCluster.mutateAsync(cluster.id)}
            />
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </Card>
  );
}
