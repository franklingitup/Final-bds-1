"use client";

import Link from "next/link";
import { ExternalLink } from "lucide-react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { DeploymentStatusBadge } from "@/components/shared/status-badge";
import type { Deployment } from "@/types/api";
import { formatRelativeTime } from "@/lib/utils";

interface DeploymentsListProps {
  deployments: Deployment[];
  orgSlug: string;
}

export function DeploymentsList({ deployments, orgSlug }: DeploymentsListProps) {
  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Image</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Replicas</TableHead>
            <TableHead>Revision</TableHead>
            <TableHead>Created</TableHead>
            <TableHead className="w-[100px]"></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {deployments.map((deployment) => (
            <TableRow key={deployment.id}>
              <TableCell className="font-mono">{deployment.image}</TableCell>
              <TableCell>
                <DeploymentStatusBadge status={deployment.status} />
              </TableCell>
              <TableCell>
                {deployment.readyReplicas}/{deployment.desiredReplicas}
              </TableCell>
              <TableCell>
                {deployment.currentRevision ? `#${deployment.currentRevision}` : "—"}
              </TableCell>
              <TableCell className="text-muted-foreground">
                {formatRelativeTime(deployment.createdAt)}
              </TableCell>
              <TableCell>
                <Button variant="ghost" size="sm" asChild>
                  <Link href={`/${orgSlug}/deployments/${deployment.id}`}>
                    View
                    <ExternalLink className="ml-2 h-3 w-3" />
                  </Link>
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
