"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { RefreshCw, RotateCcw, Activity, History, Settings, FileText, BarChart3, Loader2, Globe, Plus, Copy, Check, ShieldCheck, Trash2, AlertCircle } from "lucide-react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
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
import { observabilityApi, type LogsResponse, type ResourceMetrics } from "@/lib/api/observability";
import { domainsApi, type DomainView, VerificationStatus, CertificateStatus } from "@/lib/api/domains";
import { toast } from "@/hooks/use-toast";

// Hook to fetch deployment logs
function useDeploymentLogs(orgId: string, deploymentId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["deploymentLogs", orgId, deploymentId],
    queryFn: () =>
      observabilityApi.queryLogs(orgId, {
        // The agent ships this Kubernetes deployment ID under the
        // `deployment_id` Loki stream label.
        query: `{deployment_id="${deploymentId}"}`,
        start: "-1h",
        limit: 100,
        direction: "backward",
      }),
    enabled: enabled && !!orgId && !!deploymentId,
    refetchInterval: 30000, // Refresh every 30 seconds
  });
}

// Hook to fetch deployment metrics
function useDeploymentMetrics(orgId: string, deploymentId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["deploymentMetrics", orgId, deploymentId],
    queryFn: () => observabilityApi.getResourceMetrics(orgId, "deployment", deploymentId),
    enabled: enabled && !!orgId && !!deploymentId,
    refetchInterval: 60000, // Refresh every minute
  });
}

// Hook to fetch deployment domains
function useDeploymentDomains(orgId: string, deploymentId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["deploymentDomains", orgId, deploymentId],
    queryFn: () => domainsApi.listByDeployment(orgId, deploymentId),
    enabled: enabled && !!orgId && !!deploymentId,
  });
}

// Domain verification status badge
function DomainVerificationBadge({ status }: { status: string }) {
  const variants: Record<string, { variant: "default" | "secondary" | "destructive" | "outline"; label: string }> = {
    [VerificationStatus.VERIFIED]: { variant: "default", label: "Verified" },
    [VerificationStatus.PENDING]: { variant: "secondary", label: "Pending" },
    [VerificationStatus.VERIFYING]: { variant: "outline", label: "Verifying" },
    [VerificationStatus.FAILED]: { variant: "destructive", label: "Failed" },
  };
  const config = variants[status] || { variant: "secondary" as const, label: status };
  return <Badge variant={config.variant}>{config.label}</Badge>;
}

// Certificate status badge
function CertificateBadge({ status }: { status: string }) {
  const variants: Record<string, { variant: "default" | "secondary" | "destructive" | "outline"; label: string }> = {
    [CertificateStatus.ACTIVE]: { variant: "default", label: "Active" },
    [CertificateStatus.PENDING]: { variant: "secondary", label: "Pending" },
    [CertificateStatus.ISSUING]: { variant: "outline", label: "Issuing" },
    [CertificateStatus.EXPIRED]: { variant: "destructive", label: "Expired" },
    [CertificateStatus.FAILED]: { variant: "destructive", label: "Failed" },
  };
  const config = variants[status] || { variant: "secondary" as const, label: status };
  return <Badge variant={config.variant}>{config.label}</Badge>;
}

export default function DeploymentDetailPage() {
  const params = useParams();
  const deploymentId = params.deploymentId as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";
  const queryClient = useQueryClient();

  const { data: deployment, isLoading, error, refetch } = useDeployment(orgId, deploymentId);
  const { data: releases } = useReleases(orgId, deploymentId);
  const rollback = useRollback(orgId, deploymentId);
  
  // State for active tab to enable lazy loading of observability data
  const [activeTab, setActiveTab] = React.useState("releases");
  
  const { data: logs, isLoading: logsLoading, error: logsError } = useDeploymentLogs(
    orgId,
    deploymentId,
    activeTab === "logs"
  );
  const { data: metrics, isLoading: metricsLoading, error: metricsError } = useDeploymentMetrics(
    orgId,
    deploymentId,
    activeTab === "metrics"
  );
  
  // Domains state and queries
  const { data: domains, isLoading: domainsLoading } = useDeploymentDomains(
    orgId,
    deploymentId,
    activeTab === "domains"
  );
  
  const [newDomain, setNewDomain] = React.useState("");
  const [copiedDns, setCopiedDns] = React.useState<string | null>(null);
  
  // Domain mutations
  const createDomain = useMutation({
    mutationFn: (domain: string) => domainsApi.create(orgId, { deploymentId, domain }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["deploymentDomains", orgId, deploymentId] });
      setNewDomain("");
      toast({ title: "Domain added", description: "Configure the DNS records below to verify ownership." });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to add domain", description: error.message, variant: "destructive" });
    },
  });
  
  const deleteDomain = useMutation({
    mutationFn: (domainId: string) => domainsApi.delete(orgId, domainId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["deploymentDomains", orgId, deploymentId] });
      toast({ title: "Domain removed" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to remove domain", description: error.message, variant: "destructive" });
    },
  });
  
  const verifyDomain = useMutation({
    mutationFn: (domainId: string) => domainsApi.verify(orgId, domainId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["deploymentDomains", orgId, deploymentId] });
      if (data.verificationStatus === VerificationStatus.VERIFIED) {
        toast({ title: "Domain verified!", description: "You can now issue a TLS certificate." });
      } else {
        toast({ title: "Verification in progress", description: "DNS records not found yet. Please wait for propagation." });
      }
    },
    onError: (error: Error) => {
      toast({ title: "Verification failed", description: error.message, variant: "destructive" });
    },
  });
  
  const issueCertificate = useMutation({
    mutationFn: (domainId: string) => domainsApi.issueCertificate(orgId, domainId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["deploymentDomains", orgId, deploymentId] });
      toast({ title: "Certificate issued", description: "Your domain is now secured with TLS." });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to issue certificate", description: error.message, variant: "destructive" });
    },
  });
  
  const copyDnsValue = (value: string, id: string) => {
    navigator.clipboard.writeText(value);
    setCopiedDns(id);
    setTimeout(() => setCopiedDns(null), 2000);
  };

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
              onConfirm={async () => {
                await rollback.mutateAsync(undefined);
              }}
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

      <Tabs defaultValue="releases" value={activeTab} onValueChange={setActiveTab} className="space-y-6">
        <TabsList>
          <TabsTrigger value="releases" className="gap-2">
            <History className="h-4 w-4" />
            Releases
          </TabsTrigger>
          <TabsTrigger value="domains" className="gap-2">
            <Globe className="h-4 w-4" />
            Domains
          </TabsTrigger>
          <TabsTrigger value="logs" className="gap-2">
            <FileText className="h-4 w-4" />
            Logs
          </TabsTrigger>
          <TabsTrigger value="metrics" className="gap-2">
            <BarChart3 className="h-4 w-4" />
            Metrics
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

        <TabsContent value="domains" className="space-y-4">
          {/* Add Domain Form */}
          <Card>
            <CardHeader>
              <CardTitle>Add Custom Domain</CardTitle>
              <CardDescription>
                Connect a custom domain to this deployment
              </CardDescription>
            </CardHeader>
            <CardContent>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  if (newDomain.trim()) {
                    createDomain.mutate(newDomain.trim());
                  }
                }}
                className="flex gap-2"
              >
                <div className="flex-1">
                  <Input
                    placeholder="example.com or subdomain.example.com"
                    value={newDomain}
                    onChange={(e) => setNewDomain(e.target.value)}
                    disabled={createDomain.isPending}
                  />
                </div>
                <Button type="submit" disabled={!newDomain.trim() || createDomain.isPending}>
                  <Plus className="mr-2 h-4 w-4" />
                  Add Domain
                </Button>
              </form>
            </CardContent>
          </Card>

          {/* Domains List */}
          <Card>
            <CardHeader>
              <CardTitle>Custom Domains</CardTitle>
              <CardDescription>
                Domains attached to this deployment
              </CardDescription>
            </CardHeader>
            <CardContent>
              {domainsLoading ? (
                <div className="flex items-center justify-center py-8">
                  <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              ) : domains?.items && domains.items.length > 0 ? (
                <div className="space-y-4">
                  {domains.items.map((domain) => (
                    <Card key={domain.id} className="border-l-4 border-l-primary/20">
                      <CardHeader className="pb-2">
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <Globe className="h-4 w-4 text-muted-foreground" />
                            <span className="font-mono font-medium">{domain.fullDomain}</span>
                            {domain.isPrimary && (
                              <Badge variant="outline" className="text-xs">Primary</Badge>
                            )}
                          </div>
                          <div className="flex items-center gap-2">
                            <DomainVerificationBadge status={domain.verificationStatus} />
                            {domain.certificate && (
                              <CertificateBadge status={domain.certificate.status} />
                            )}
                          </div>
                        </div>
                      </CardHeader>
                      <CardContent className="space-y-4">
                        {/* DNS Records for verification */}
                        {domain.verificationStatus !== VerificationStatus.VERIFIED && (
                          <div className="rounded-md border bg-muted/30 p-4 space-y-3">
                            <div className="flex items-center gap-2 text-sm font-medium">
                              <AlertCircle className="h-4 w-4 text-yellow-600" />
                              DNS Configuration Required
                            </div>
                            <p className="text-sm text-muted-foreground">
                              Add the following DNS TXT record to verify domain ownership:
                            </p>
                            <div className="grid gap-2">
                              <div className="flex items-center gap-2">
                                <Label className="w-20 text-xs text-muted-foreground">Name:</Label>
                                <code className="flex-1 px-2 py-1 bg-background rounded text-sm font-mono">
                                  {domain.dnsRecords.txtName}
                                </code>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => copyDnsValue(domain.dnsRecords.txtName, `${domain.id}-name`)}
                                >
                                  {copiedDns === `${domain.id}-name` ? (
                                    <Check className="h-4 w-4" />
                                  ) : (
                                    <Copy className="h-4 w-4" />
                                  )}
                                </Button>
                              </div>
                              <div className="flex items-center gap-2">
                                <Label className="w-20 text-xs text-muted-foreground">Value:</Label>
                                <code className="flex-1 px-2 py-1 bg-background rounded text-sm font-mono truncate">
                                  {domain.dnsRecords.txtValue}
                                </code>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => copyDnsValue(domain.dnsRecords.txtValue, `${domain.id}-value`)}
                                >
                                  {copiedDns === `${domain.id}-value` ? (
                                    <Check className="h-4 w-4" />
                                  ) : (
                                    <Copy className="h-4 w-4" />
                                  )}
                                </Button>
                              </div>
                            </div>
                          </div>
                        )}

                        {/* Certificate info */}
                        {domain.certificate && domain.certificate.status === CertificateStatus.ACTIVE && (
                          <div className="flex items-center gap-2 text-sm text-green-600">
                            <ShieldCheck className="h-4 w-4" />
                            <span>
                              TLS certificate active
                              {domain.certificate.daysToExpiry !== undefined && (
                                <span className="text-muted-foreground ml-1">
                                  (expires in {domain.certificate.daysToExpiry} days)
                                </span>
                              )}
                            </span>
                          </div>
                        )}

                        {/* Actions */}
                        <div className="flex items-center gap-2 pt-2">
                          {domain.verificationStatus !== VerificationStatus.VERIFIED && (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => verifyDomain.mutate(domain.id)}
                              disabled={verifyDomain.isPending}
                            >
                              {verifyDomain.isPending ? (
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                              ) : (
                                <RefreshCw className="mr-2 h-4 w-4" />
                              )}
                              Verify DNS
                            </Button>
                          )}
                          {domain.verificationStatus === VerificationStatus.VERIFIED && !domain.certificate && (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => issueCertificate.mutate(domain.id)}
                              disabled={issueCertificate.isPending}
                            >
                              {issueCertificate.isPending ? (
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                              ) : (
                                <ShieldCheck className="mr-2 h-4 w-4" />
                              )}
                              Issue Certificate
                            </Button>
                          )}
                          {domain.verificationStatus === VerificationStatus.VERIFIED && 
                           domain.certificate?.status === CertificateStatus.EXPIRED && (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => issueCertificate.mutate(domain.id)}
                              disabled={issueCertificate.isPending}
                            >
                              {issueCertificate.isPending ? (
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                              ) : (
                                <RefreshCw className="mr-2 h-4 w-4" />
                              )}
                              Renew Certificate
                            </Button>
                          )}
                          <ConfirmDialog
                            trigger={
                              <Button variant="ghost" size="sm" className="text-destructive">
                                <Trash2 className="mr-2 h-4 w-4" />
                                Remove
                              </Button>
                            }
                            title="Remove Domain"
                            description={`Remove ${domain.fullDomain} from this deployment? This will also delete any associated certificates.`}
                            confirmLabel="Remove"
                            variant="destructive"
                            onConfirm={() => deleteDomain.mutate(domain.id)}
                          />
                        </div>
                      </CardContent>
                    </Card>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground text-center py-8">
                  No custom domains configured. Add a domain above to get started.
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="logs" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Recent Logs</CardTitle>
              <CardDescription>
                Log output from this deployment (last hour)
              </CardDescription>
            </CardHeader>
            <CardContent>
              {logsLoading ? (
                <div className="flex items-center justify-center py-8">
                  <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              ) : logsError ? (
                <p className="text-sm text-muted-foreground text-center py-8">
                  Unable to load logs. Observability backend may not be configured.
                </p>
              ) : logs?.data?.result && logs.data.result.length > 0 ? (
                <div className="max-h-[500px] overflow-auto rounded-md border bg-muted/30 p-4">
                  <pre className="font-mono text-xs leading-relaxed whitespace-pre-wrap">
                    {logs.data.result
                      .flatMap((stream) =>
                        stream.values.map(([timestamp, line]) => {
                          const date = new Date(parseInt(timestamp) / 1000000);
                          return `[${date.toLocaleTimeString()}] ${line}`;
                        })
                      )
                      .join("\n")}
                  </pre>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground text-center py-8">
                  No logs found for this deployment in the last hour
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="metrics" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Resource Metrics</CardTitle>
              <CardDescription>
                Current resource utilization for this deployment
              </CardDescription>
            </CardHeader>
            <CardContent>
              {metricsLoading ? (
                <div className="flex items-center justify-center py-8">
                  <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              ) : metricsError ? (
                <p className="text-sm text-muted-foreground text-center py-8">
                  Unable to load metrics. Observability backend may not be configured.
                </p>
              ) : metrics && Object.keys(metrics.metrics).length > 0 ? (
                <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
                  {Object.entries(metrics.metrics).map(([name, metric]) => (
                    <Card key={name}>
                      <CardHeader className="pb-2">
                        <CardDescription className="capitalize">
                          {name.replace(/_/g, " ")}
                        </CardDescription>
                        <CardTitle className="text-2xl">
                          {metric.value.toFixed(2)}
                          {metric.unit && (
                            <span className="text-sm font-normal text-muted-foreground ml-1">
                              {metric.unit}
                            </span>
                          )}
                        </CardTitle>
                      </CardHeader>
                      {metric.threshold && (
                        <CardContent className="pt-0">
                          <span
                            className={`text-xs font-medium ${
                              metric.threshold === "critical"
                                ? "text-destructive"
                                : metric.threshold === "warning"
                                ? "text-yellow-600"
                                : "text-green-600"
                            }`}
                          >
                            {metric.threshold.toUpperCase()}
                          </span>
                        </CardContent>
                      )}
                    </Card>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground text-center py-8">
                  No metrics available for this deployment
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
