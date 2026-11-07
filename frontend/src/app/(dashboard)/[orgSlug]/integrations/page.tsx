"use client";

import * as React from "react";
import {
  Github,
  Key,
  RefreshCw,
  Unlink,
  Loader2,
  Check,
  Plus,
  Plug,
} from "lucide-react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useOrganization } from "@/providers/organization-provider";
import { githubApi, type ConnectionView, ConnectionStatus } from "@/lib/api/github";
import { toast } from "@/hooks/use-toast";

// ─── GitHub integration panel ─────────────────────────────────────────────────

function GitHubIntegration({ orgId }: { orgId: string }) {
  const queryClient = useQueryClient();
  const [showPATForm, setShowPATForm] = React.useState(false);
  const [patName, setPatName] = React.useState("");
  const [patToken, setPatToken] = React.useState("");

  const { data: connections, isLoading } = useQuery({
    queryKey: ["github-connections", orgId],
    queryFn: () => githubApi.listConnections(orgId),
    enabled: !!orgId,
  });

  const createPAT = useMutation({
    mutationFn: () =>
      githubApi.createPATConnection(orgId, { name: patName, accessToken: patToken }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["github-connections", orgId] });
      setShowPATForm(false);
      setPatName("");
      setPatToken("");
      toast({ title: "GitHub connected via PAT" });
    },
    onError: (err: Error) =>
      toast({ title: "Failed to connect GitHub", description: err.message, variant: "destructive" }),
  });

  const deleteConnection = useMutation({
    mutationFn: (id: string) => githubApi.deleteConnection(orgId, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["github-connections", orgId] });
      toast({ title: "GitHub connection removed" });
    },
    onError: (err: Error) =>
      toast({ title: "Failed to remove connection", description: err.message, variant: "destructive" }),
  });

  const validateConnection = useMutation({
    mutationFn: (id: string) => githubApi.validateConnection(orgId, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["github-connections", orgId] });
      toast({ title: "Connection validated" });
    },
    onError: (err: Error) =>
      toast({ title: "Validation failed", description: err.message, variant: "destructive" }),
  });

  const handleOAuth = async () => {
    try {
      const { url } = await githubApi.getOAuthURL(orgId, window.location.href);
      window.location.href = url;
    } catch (err) {
      toast({
        title: "Failed to start GitHub OAuth",
        description: (err as Error).message,
        variant: "destructive",
      });
    }
  };

  const hasConnections = (connections?.items?.length ?? 0) > 0;

  return (
    <div className="space-y-4">
      {/* Existing connections */}
      {isLoading ? (
        <div className="flex items-center justify-center py-6">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : hasConnections ? (
        <div className="space-y-2">
          {connections!.items.map((conn: ConnectionView) => (
            <div
              key={conn.id}
              className="flex items-center justify-between rounded-lg border p-3"
            >
              <div className="flex items-center gap-3 min-w-0">
                {conn.githubAvatar ? (
                  <img
                    src={conn.githubAvatar}
                    alt=""
                    className="h-8 w-8 rounded-full shrink-0"
                  />
                ) : (
                  <Github className="h-8 w-8 shrink-0 rounded-full bg-muted p-1" />
                )}
                <div className="min-w-0">
                  <p className="font-medium truncate">{conn.name}</p>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    {conn.githubUsername && <span>@{conn.githubUsername}</span>}
                    <Badge variant="outline" className="text-xs capitalize">
                      {conn.connectionType}
                    </Badge>
                    <Badge
                      variant={
                        conn.status === ConnectionStatus.ACTIVE ? "default" : "destructive"
                      }
                      className="text-xs"
                    >
                      {conn.status}
                    </Badge>
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => validateConnection.mutate(conn.id)}
                  disabled={validateConnection.isPending}
                  title="Re-validate connection"
                >
                  <RefreshCw
                    className={`h-4 w-4 ${validateConnection.isPending ? "animate-spin" : ""}`}
                  />
                </Button>
                <ConfirmDialog
                  trigger={
                    <Button variant="ghost" size="sm" className="text-destructive" title="Remove connection">
                      <Unlink className="h-4 w-4" />
                    </Button>
                  }
                  title="Remove GitHub Connection"
                  description="This removes the connection. Already-imported repositories will remain in your projects."
                  confirmLabel="Remove"
                  variant="destructive"
                  onConfirm={() => deleteConnection.mutate(conn.id)}
                />
              </div>
            </div>
          ))}
        </div>
      ) : null}

      {/* Add connection controls */}
      {!showPATForm ? (
        <div className="flex flex-wrap gap-3">
          <Button onClick={handleOAuth}>
            <Github className="mr-2 h-4 w-4" />
            {hasConnections ? "Add GitHub account via OAuth" : "Connect with GitHub OAuth"}
          </Button>
          <Button variant="outline" onClick={() => setShowPATForm(true)}>
            <Key className="mr-2 h-4 w-4" />
            {hasConnections ? "Add via Personal Access Token" : "Use Personal Access Token"}
          </Button>
        </div>
      ) : (
        <div className="rounded-lg border bg-muted/50 p-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="pat-name">Connection Name</Label>
            <Input
              id="pat-name"
              placeholder="e.g. My GitHub PAT"
              value={patName}
              onChange={(e) => setPatName(e.target.value)}
              autoFocus
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="pat-token">Personal Access Token</Label>
            <Input
              id="pat-token"
              type="password"
              placeholder="ghp_xxxxxxxxxxxx"
              value={patToken}
              onChange={(e) => setPatToken(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Generate at GitHub → Settings → Developer settings → Personal access tokens
            </p>
          </div>
          <div className="flex gap-2">
            <Button
              onClick={() => createPAT.mutate()}
              disabled={!patName || !patToken || createPAT.isPending}
            >
              {createPAT.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Connect
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setShowPATForm(false);
                setPatName("");
                setPatToken("");
              }}
            >
              Cancel
            </Button>
          </div>
        </div>
      )}

      {hasConnections && (
        <p className="text-xs text-muted-foreground">
          Connected accounts are shared across the organization. Use repositories from these
          connections inside any project&apos;s{" "}
          <strong>Repositories</strong> tab.
        </p>
      )}
    </div>
  );
}

// ─── Integration card wrapper (extensible — add Slack, etc. here later) ───────

interface IntegrationCardProps {
  icon: React.ReactNode;
  name: string;
  description: string;
  status: "connected" | "not-connected" | "loading";
  children: React.ReactNode;
}

function IntegrationCard({
  icon,
  name,
  description,
  status,
  children,
}: IntegrationCardProps) {
  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-start gap-4">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg border bg-muted">
            {icon}
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <CardTitle className="text-lg">{name}</CardTitle>
              {status === "loading" ? (
                <Badge variant="secondary" className="text-xs">
                  <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                  Loading
                </Badge>
              ) : status === "connected" ? (
                <Badge variant="default" className="text-xs">
                  <Check className="mr-1 h-3 w-3" />
                  Connected
                </Badge>
              ) : (
                <Badge variant="secondary" className="text-xs">Not connected</Badge>
              )}
            </div>
            <CardDescription className="mt-1">{description}</CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}

// ─── Page ─────────────────────────────────────────────────────────────────────

export default function IntegrationsPage() {
  const { organization } = useOrganization();
  const orgId = organization?.id || "";

  // Determine GitHub connection status for the card badge
  const { data: githubConnections, isLoading: githubLoading } = useQuery({
    queryKey: ["github-connections", orgId],
    queryFn: () => githubApi.listConnections(orgId),
    enabled: !!orgId,
  });

  const githubStatus: IntegrationCardProps["status"] = githubLoading
    ? "loading"
    : (githubConnections?.items?.length ?? 0) > 0
    ? "connected"
    : "not-connected";

  if (!organization) return <LoadingState />;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Integrations"
        description="Connect third-party services to your organization"
      />

      {/* Integration grid — add new cards below as integrations are added */}
      <div className="grid gap-6">
        <IntegrationCard
          icon={<Github className="h-6 w-6" />}
          name="GitHub"
          description="Import repositories and trigger builds from GitHub pushes and pull requests."
          status={githubStatus}
        >
          <GitHubIntegration orgId={orgId} />
        </IntegrationCard>

        {/* ── Placeholder cards — remove or replace when real integrations ship ── */}
        <IntegrationCard
          icon={<Plug className="h-6 w-6 text-muted-foreground" />}
          name="Slack"
          description="Receive deployment and build notifications in your Slack workspace."
          status="not-connected"
        >
          <p className="text-sm text-muted-foreground">Coming soon.</p>
        </IntegrationCard>
      </div>
    </div>
  );
}
