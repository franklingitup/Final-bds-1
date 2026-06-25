"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { RefreshCw, RotateCcw, Activity, History, Settings } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { DeploymentStatusBadge, ReleaseStatusBadge } from "@/components/shared/status-badge";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useDeployment, useReleases, useRollback } from "@/hooks/use-deployments";
import { useOrganization } from "@/providers/organization-provider";
import { formatDate, formatRelativeTime } from "@/lib/utils";

export default function DeploymentDetailPage() {
  const params = useParams();
  const deploymentId = params.deploymentId as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";

  const { data: deployment, isLoading, error, refetch } = useDeployment(orgId, deploymentId);
  const { data: releases } = useReleases(orgId, deploymentId);
  const rollback = useRollback(orgId, deploymentId);

  if (isLoading) {
    return <LoadingState message="Loading deployment..." />;
  }

  if (error || !deployment) {
    return <ErrorState onRetry={() => refetch()} />;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={deployment.image.split(":")[0].split("/").pop() || "Deployment"}
        description={deployment.image}
        action={
          <div className="flex items-center gap-2">
            <DeploymentStatusBadge status={deployment.status} />
            <ConfirmDialog
              trigger={
                <Button variant="outline">
                  <RotateCcw className="mr-2 h-4 w-4" />
                  Rollback
                </Button>
              }
              title="Rollback Deployment"
              description="This will rollback to the previous release. Are you sure?"
              confirmLabel="Rollback"
              onConfirm={() => rollback.mutateAsync()}
            />
          </div>
        }
      />

      <div className="grid gap-6 md:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Status</CardDescription>
            <CardTitle className="text-2xl capitalize">{deployment.status}</CardTitle>
          </CardHeader>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Replicas</CardDescription>
            <CardTitle className="text-2xl">
              {deployment.readyReplicas}/{deployment.desiredReplicas}
            </CardTitle>
          </CardHeader>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Current Revision</CardDescription>
            <CardTitle className="text-2xl">
              #{deployment.currentRevision || "—"}
            </CardTitle>
          </CardHeader>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Created</CardDescription>
            <CardTitle className="text-2xl">
              {formatRelativeTime(deployment.createdAt)}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Tabs defaultValue="releases" className="space-y-6">
        <TabsList>
          <TabsTrigger value="releases" className="gap-2">
            <History className="h-4 w-4" />
            Releases
          </TabsTrigger>
          <TabsTrigger value="config" className="gap-2">
            <Settings className="h-4 w-4" />
            Configuration
          </TabsTrigger>
        </TabsList>

        <TabsContent value="releases" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Release History</CardTitle>
              <CardDescription>
                All revisions of this deployment
              </CardDescription>
            </CardHeader>
            <CardContent>
              {releases?.items && releases.items.length > 0 ? (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Revision</TableHead>
                      <TableHead>Image</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Started</TableHead>
                      <TableHead>Finished</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {releases.items.map((release) => (
                      <TableRow key={release.id}>
                        <TableCell className="font-medium">
                          #{release.revision}
                          {release.revision === deployment.currentRevision && (
                            <span className="ml-2 text-xs text-muted-foreground">
                              (current)
                            </span>
                          )}
                        </TableCell>
                        <TableCell className="font-mono text-sm">
                          {release.image}
                        </TableCell>
                        <TableCell>
                          <ReleaseStatusBadge status={release.status} />
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {release.startedAt
                            ? formatRelativeTime(release.startedAt)
                            : "—"}
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {release.finishedAt
                            ? formatRelativeTime(release.finishedAt)
                            : "—"}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              ) : (
                <p className="text-sm text-muted-foreground text-center py-8">
                  No releases yet
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="config" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Resource Configuration</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="grid grid-cols-2 gap-4">
                <div>
                  <dt className="text-sm text-muted-foreground">CPU Request</dt>
                  <dd className="font-mono">{deployment.cpuRequest || "—"}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">CPU Limit</dt>
                  <dd className="font-mono">{deployment.cpuLimit || "—"}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Memory Request</dt>
                  <dd className="font-mono">{deployment.memoryRequest || "—"}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Memory Limit</dt>
                  <dd className="font-mono">{deployment.memoryLimit || "—"}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Port</dt>
                  <dd className="font-mono">{deployment.port || "—"}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Replicas</dt>
                  <dd className="font-mono">{deployment.replicas}</dd>
                </div>
              </dl>
            </CardContent>
          </Card>

          {deployment.envVars && deployment.envVars.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>Environment Variables</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Value</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {deployment.envVars.map((env, i) => (
                      <TableRow key={i}>
                        <TableCell className="font-mono">{env.name}</TableCell>
                        <TableCell className="font-mono text-muted-foreground">
                          ••••••••
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
