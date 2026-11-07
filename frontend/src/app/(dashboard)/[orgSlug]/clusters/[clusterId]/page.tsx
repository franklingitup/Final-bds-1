"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { Copy, RefreshCw, Terminal, Server, Activity, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { ClusterStatusBadge } from "@/components/shared/status-badge";
import { CodeBlock } from "@/components/shared/code-block";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useCluster, useGenerateRegistrationToken, useClusterHeartbeats } from "@/hooks/use-clusters";
import { useOrganization } from "@/providers/organization-provider";
import { getInstallCommand } from "@/lib/api";
import { formatDate, formatRelativeTime, copyToClipboard } from "@/lib/utils";
import { toast } from "@/hooks/use-toast";

export default function ClusterDetailPage() {
  const params = useParams();
  const clusterId = params.clusterId as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";

  const { data: cluster, isLoading, error, refetch } = useCluster(orgId, clusterId);
  const { data: heartbeats } = useClusterHeartbeats(orgId, clusterId);
  const generateToken = useGenerateRegistrationToken(orgId, clusterId);

  const [token, setToken] = React.useState<string | null>(null);
  const [manifestUrl, setManifestUrl] = React.useState<string | null>(null);

  const handleGenerateToken = async (force?: boolean) => {
    // Control plane endpoint is now sourced server-side from CONTROL_PLANE_PUBLIC_URL,
    // not the browser's API base URL (which is only reachable from the browser, not
    // from inside customer Kubernetes clusters).
    const result = await generateToken.mutateAsync({
      expiresIn: "24h",
      force,
    });
    setToken(result.token);
    setManifestUrl(result.manifestUrl);
    toast({
      title: force
        ? "Registration token regenerated"
        : "Registration token generated",
      description: force
        ? "The previous agent connection has been superseded. Install the new agent."
        : undefined,
    });
    // Refetch cluster to update status badge if it changed
    refetch();
  };

  const handleCopyToken = async () => {
    if (token) {
      await copyToClipboard(token);
      toast({ title: "Token copied to clipboard" });
    }
  };

  if (isLoading) {
    return <LoadingState message="Loading cluster..." />;
  }

  if (error || !cluster) {
    return <ErrorState onRetry={() => refetch()} />;
  }

  const installCommand = manifestUrl ? getInstallCommand(manifestUrl) : null;

  return (
    <div className="space-y-6">
      <PageHeader
        title={cluster.name}
        description={cluster.description || "No description"}
        action={<ClusterStatusBadge status={cluster.status} />}
      />

      <div className="grid gap-6 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Status</CardDescription>
            <CardTitle className="text-2xl flex items-center gap-2">
              <Server className="h-5 w-5" />
              {cluster.status === "connected" ? "Connected" : "Disconnected"}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {cluster.lastHeartbeatAt && (
              <p className="text-sm text-muted-foreground">
                Last seen {formatRelativeTime(cluster.lastHeartbeatAt)}
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Kubernetes Version</CardDescription>
            <CardTitle className="text-2xl">
              {cluster.kubernetesVersion || "Unknown"}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              {cluster.cloudProvider || "Unknown provider"}
              {cluster.region && ` / ${cluster.region}`}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Nodes</CardDescription>
            <CardTitle className="text-2xl">
              {cluster.nodeCount || 0}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              Registered {cluster.registeredAt ? formatDate(cluster.registeredAt) : "—"}
            </p>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="install" className="space-y-6">
        <TabsList>
          <TabsTrigger value="install" className="gap-2">
            <Terminal className="h-4 w-4" />
            Installation
          </TabsTrigger>
          <TabsTrigger value="heartbeats" className="gap-2">
            <Activity className="h-4 w-4" />
            Heartbeats
          </TabsTrigger>
        </TabsList>

        <TabsContent value="install" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Install Platform Agent</CardTitle>
              <CardDescription>
                {cluster.status === "pending"
                  ? "Generate an installation command, then run it once against your cluster"
                  : cluster.status === "connected"
                  ? "Agent is connected and healthy"
                  : "Agent is disconnected — regenerate the token to reinstall"}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {/* Pending: first-time installation */}
              {cluster.status === "pending" && (
                <>
                  <div className="flex items-center gap-2">
                    <Button onClick={() => handleGenerateToken()} disabled={generateToken.isPending}>
                      <RefreshCw className="mr-2 h-4 w-4" />
                      Generate Token
                    </Button>
                    {token && (
                      <Button variant="outline" onClick={handleCopyToken}>
                        <Copy className="mr-2 h-4 w-4" />
                        Copy Token
                      </Button>
                    )}
                  </div>

                  {token && (
                    <div className="space-y-2">
                      <p className="text-sm font-medium">Registration Token</p>
                      <CodeBlock code={token} />
                    </div>
                  )}

                  {installCommand && (
                    <div className="space-y-2">
                      <p className="text-sm font-medium">
                        Run this one-line command with kubectl configured for the target cluster
                      </p>
                      <CodeBlock code={installCommand} language="bash" />
                    </div>
                  )}
                </>
              )}

              {/* Connected: show status + offer force-regeneration */}
              {cluster.status === "connected" && !token && (
                <div className="space-y-6">
                  <div className="text-center py-4">
                    <Server className="mx-auto h-12 w-12 text-green-500" />
                    <h3 className="mt-4 text-lg font-semibold">Agent Connected</h3>
                    <p className="mt-2 text-sm text-muted-foreground">
                      Agent ID: {cluster.agentId}
                    </p>
                  </div>

                  <div className="border-t pt-4">
                    <h4 className="text-sm font-medium mb-2">Need to reinstall the agent?</h4>
                    <p className="text-sm text-muted-foreground mb-4">
                      If the agent is malfunctioning or you need to move it to a different cluster,
                      you can regenerate the registration token. This will invalidate the current
                      agent connection.
                    </p>
                    <ConfirmDialog
                      trigger={
                        <Button variant="outline">
                          <RefreshCw className="mr-2 h-4 w-4" />
                          Regenerate Token
                        </Button>
                      }
                      title="Regenerate Registration Token?"
                      description="This will supersede the currently connected agent. The existing agent will stop working and you'll need to reinstall using the new token. This action cannot be undone."
                      confirmLabel="Regenerate Token"
                      variant="destructive"
                      onConfirm={() => handleGenerateToken(true)}
                      isLoading={generateToken.isPending}
                    />
                  </div>
                </div>
              )}

              {/* Disconnected: prompt to regenerate */}
              {cluster.status === "disconnected" && !token && (
                <div className="space-y-6">
                  <div className="text-center py-4">
                    <AlertTriangle className="mx-auto h-12 w-12 text-yellow-500" />
                    <h3 className="mt-4 text-lg font-semibold">Agent Disconnected</h3>
                    <p className="mt-2 text-sm text-muted-foreground">
                      Last agent ID: {cluster.agentId || "Unknown"}
                    </p>
                    {cluster.lastHeartbeatAt && (
                      <p className="text-sm text-muted-foreground">
                        Last seen {formatRelativeTime(cluster.lastHeartbeatAt)}
                      </p>
                    )}
                  </div>

                  <div className="border-t pt-4">
                    <p className="text-sm text-muted-foreground mb-4">
                      The agent has stopped sending heartbeats. You can regenerate the registration
                      token to reinstall the agent on this cluster.
                    </p>
                    <Button onClick={() => handleGenerateToken(true)} disabled={generateToken.isPending}>
                      <RefreshCw className="mr-2 h-4 w-4" />
                      Regenerate Token
                    </Button>
                  </div>
                </div>
              )}

              {/* After regeneration: show the new install command */}
              {(cluster.status === "connected" || cluster.status === "disconnected") && token && (
                <div className="space-y-4">
                  <div className="rounded-md bg-yellow-50 border border-yellow-200 p-4">
                    <div className="flex">
                      <AlertTriangle className="h-5 w-5 text-yellow-600 mr-3 flex-shrink-0" />
                      <div>
                        <h4 className="text-sm font-medium text-yellow-800">
                          New token generated
                        </h4>
                        <p className="mt-1 text-sm text-yellow-700">
                          The previous agent connection has been superseded. Apply the command
                          below to install the new agent.
                        </p>
                      </div>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <p className="text-sm font-medium">Registration Token</p>
                    <CodeBlock code={token} />
                  </div>

                  {installCommand && (
                    <div className="space-y-2">
                      <p className="text-sm font-medium">
                        Run this one-line command with kubectl configured for the target cluster
                      </p>
                      <CodeBlock code={installCommand} language="bash" />
                    </div>
                  )}

                  <Button variant="outline" onClick={handleCopyToken}>
                    <Copy className="mr-2 h-4 w-4" />
                    Copy Token
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="heartbeats" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Recent Heartbeats</CardTitle>
              <CardDescription>
                Health updates from the platform agent
              </CardDescription>
            </CardHeader>
            <CardContent>
              {heartbeats?.items && heartbeats.items.length > 0 ? (
                <div className="space-y-2">
                  {heartbeats.items.map((hb, i) => (
                    <div
                      key={i}
                      className="flex items-center justify-between border-b py-2 last:border-0"
                    >
                      <div>
                        <p className="text-sm font-medium">
                          {hb.kubernetesVersion} - {hb.nodeCount} nodes
                        </p>
                        <p className="text-xs text-muted-foreground">
                          API Server: {hb.apiServerHealthy ? "Healthy" : "Unhealthy"}
                        </p>
                      </div>
                      <p className="text-sm text-muted-foreground">
                        {formatRelativeTime(hb.receivedAt)}
                      </p>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground text-center py-8">
                  No heartbeats received yet
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
