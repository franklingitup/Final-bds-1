"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { Copy, RefreshCw, Terminal, Server, Activity } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { ClusterStatusBadge } from "@/components/shared/status-badge";
import { CodeBlock } from "@/components/shared/code-block";
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

  const handleGenerateToken = async () => {
    const result = await generateToken.mutateAsync({ expiresIn: "24h" });
    setToken(result.token);
    toast({ title: "Registration token generated" });
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

  const installCommand = token
    ? getInstallCommand(token, process.env.NEXT_PUBLIC_API_URL || "https://api.bdsplatform.io")
    : null;

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
                Generate a registration token and install the platform agent in your cluster
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {cluster.status === "pending" ? (
                <>
                  <div className="flex items-center gap-2">
                    <Button onClick={handleGenerateToken} loading={generateToken.isPending}>
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
                      <p className="text-sm font-medium">Install Command</p>
                      <CodeBlock code={installCommand} language="bash" />
                    </div>
                  )}
                </>
              ) : (
                <div className="text-center py-8">
                  <Server className="mx-auto h-12 w-12 text-green-500" />
                  <h3 className="mt-4 text-lg font-semibold">Agent Connected</h3>
                  <p className="mt-2 text-sm text-muted-foreground">
                    Agent ID: {cluster.agentId}
                  </p>
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
