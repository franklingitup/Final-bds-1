"use client";

import Link from "next/link";
import { ExternalLink, MoreHorizontal, Trash2 } from "lucide-react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { DeploymentStatusBadge } from "@/components/shared/status-badge";
import { useDeleteDeployment } from "@/hooks/use-deployments";
import { useOrganization } from "@/providers/organization-provider";
import type { Deployment } from "@/types/api";
import { formatRelativeTime } from "@/lib/utils";

interface DeploymentsListProps {
  deployments: Deployment[];
  orgSlug: string;
}

export function DeploymentsList({ deployments, orgSlug }: DeploymentsListProps) {
  const { organization } = useOrganization();
  const deleteDeployment = useDeleteDeployment(organization?.id || "");

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
            <TableHead className="w-[140px]"></TableHead>
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
              <TableCell className="flex items-center justify-end">
                <Button variant="ghost" size="sm" asChild>
                  <Link href={`/${orgSlug}/deployments/${deployment.id}`}>
                    View
                    <ExternalLink className="ml-2 h-3 w-3" />
                  </Link>
                </Button>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Actions for deployment ${deployment.id}`}
                      disabled={deleteDeployment.isPending}
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
                          disabled={deleteDeployment.isPending}
                        >
                          <Trash2 className="h-4 w-4" />
                          {deleteDeployment.isPending ? "Deleting..." : "Delete"}
                        </DropdownMenuItem>
                      }
                      title="Delete Deployment"
                      description="Delete this deployment? This action cannot be undone."
                      confirmLabel="Delete"
                      variant="destructive"
                      isLoading={deleteDeployment.isPending}
                      onConfirm={() => deleteDeployment.mutateAsync(deployment.id)}
                    />
                  </DropdownMenuContent>
                </DropdownMenu>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
