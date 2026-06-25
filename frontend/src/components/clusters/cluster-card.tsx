import Link from "next/link";
import { Server, Cpu, HardDrive } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ClusterStatusBadge } from "@/components/shared/status-badge";
import type { Cluster } from "@/types/api";
import { formatRelativeTime } from "@/lib/utils";

interface ClusterCardProps {
  cluster: Cluster;
  orgSlug: string;
}

export function ClusterCard({ cluster, orgSlug }: ClusterCardProps) {
  return (
    <Link href={`/${orgSlug}/clusters/${cluster.id}`}>
      <Card className="cursor-pointer transition-colors hover:bg-accent/50">
        <CardHeader>
          <div className="flex items-center justify-between">
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
      </Card>
    </Link>
  );
}
