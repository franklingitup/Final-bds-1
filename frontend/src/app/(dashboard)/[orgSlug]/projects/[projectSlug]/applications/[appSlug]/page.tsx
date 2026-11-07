"use client";

import * as React from "react";
import { useParams, useRouter } from "next/navigation";
import {
  Plus,
  Layers,
  Pencil,
  Trash2,
  Loader2,
  Server,
  ArrowLeft,
  AlertTriangle,
  Box,
  PackageCheck,
} from "lucide-react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { EmptyState } from "@/components/shared/empty-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useProjectBySlug } from "@/hooks/use-projects";
import { useApplications } from "@/hooks/use-applications";
import { useClusters } from "@/hooks/use-clusters";
import {
  useEnvironments,
  useCreateEnvironment,
  useUpdateEnvironment,
  useDeleteEnvironment,
} from "@/hooks/use-environments";
import { useOrganization } from "@/providers/organization-provider";
import { deploymentsApi } from "@/lib/api";
import type {
  Environment,
  Deployment,
  CreateEnvironmentRequest,
  UpdateEnvironmentRequest,
  Cluster,
} from "@/types/api";

// ---------------------------------------------------------------------------
// Cluster-collision warning
// ---------------------------------------------------------------------------

/**
 * Returns true when exactly one of the two names contains "prod"
 * (case-insensitive) — i.e. the XOR of the two "isProd" booleans is true.
 * This detects the case where production and non-production environments
 * would share a cluster.
 */
function isProdName(name: string): boolean {
  return name.toLowerCase().includes("prod");
}

function clusterCollisionWarning(
  newName: string,
  selectedClusterId: string,
  existingEnvs: Environment[],
  /** Optional: the id of the environment currently being edited (excluded from check). */
  excludeEnvId?: string
): string | null {
  if (!newName.trim() || !selectedClusterId) return null;

  const conflict = existingEnvs.find(
    (e) =>
      e.id !== excludeEnvId &&
      e.clusterId === selectedClusterId &&
      isProdName(e.name) !== isProdName(newName)
  );

  if (!conflict) return null;

  return (
    "Using the same cluster for production and non-production environments is not " +
    "recommended for production-grade deployments. Consider using separate clusters " +
    "for stronger isolation."
  );
}

// ---------------------------------------------------------------------------
// Create Environment Dialog
// ---------------------------------------------------------------------------

interface CreateEnvDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  appId: string;
  existingEnvs: Environment[];
  clusters: Cluster[];
}

function CreateEnvironmentDialog({
  open,
  onOpenChange,
  orgId,
  appId,
  existingEnvs,
  clusters,
}: CreateEnvDialogProps) {
  const [name, setName] = React.useState("");
  const [clusterId, setClusterId] = React.useState("");
  const [namespace, setNamespace] = React.useState("");

  const warning = clusterCollisionWarning(name, clusterId, existingEnvs);

  const createMutation = useCreateEnvironment(orgId, appId);

  function reset() {
    setName("");
    setClusterId("");
    setNamespace("");
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim() || !clusterId || !namespace.trim()) return;

    const req: CreateEnvironmentRequest = {
      name: name.trim(),
      clusterId,
      namespace: namespace.trim(),
      orderIndex: existingEnvs.length, // append at end
    };

    await createMutation.mutateAsync(req, {
      onSuccess: () => {
        reset();
        onOpenChange(false);
      },
    });
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) reset();
        onOpenChange(v);
      }}
    >
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle>New Environment</DialogTitle>
          <DialogDescription>
            Define where this application is deployed. Each environment is bound
            to a specific Kubernetes cluster and namespace.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Name */}
          <div className="space-y-2">
            <Label htmlFor="env-name">Name</Label>
            <Input
              id="env-name"
              placeholder="staging"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
            <p className="text-xs text-muted-foreground">
              e.g. staging, production, preview. Must be unique within this application.
            </p>
          </div>

          {/* Cluster selector */}
          <div className="space-y-2">
            <Label htmlFor="env-cluster">Cluster</Label>
            <Select value={clusterId} onValueChange={setClusterId}>
              <SelectTrigger id="env-cluster">
                <SelectValue placeholder="Select a cluster…" />
              </SelectTrigger>
              <SelectContent>
                {clusters.length === 0 && (
                  <SelectItem value="__none__" disabled>
                    No connected clusters
                  </SelectItem>
                )}
                {clusters.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    <div className="flex items-center gap-2">
                      <Server className="h-3.5 w-3.5 text-muted-foreground" />
                      <span>{c.name}</span>
                      <Badge
                        variant={c.status === "connected" ? "default" : "secondary"}
                        className="text-xs ml-1"
                      >
                        {c.status}
                      </Badge>
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Cluster collision warning — prominent but non-blocking */}
          {warning && (
            <div className="flex gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
              <AlertTriangle className="h-4 w-4 mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />
              <p>{warning}</p>
            </div>
          )}

          {/* Namespace */}
          <div className="space-y-2">
            <Label htmlFor="env-namespace">Kubernetes Namespace</Label>
            <Input
              id="env-namespace"
              placeholder="my-app-staging"
              value={namespace}
              onChange={(e) => setNamespace(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              The Kubernetes namespace where workloads for this environment will be deployed.
              Must be lowercase alphanumeric with hyphens.
            </p>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                reset();
                onOpenChange(false);
              }}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                !name.trim() ||
                !clusterId ||
                !namespace.trim() ||
                createMutation.isPending
              }
            >
              {createMutation.isPending && (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              )}
              Create Environment
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Edit Environment Dialog
// ---------------------------------------------------------------------------

interface EditEnvDialogProps {
  environment: Environment | null;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  appId: string;
}

function EditEnvironmentDialog({
  environment,
  onOpenChange,
  orgId,
  appId,
}: EditEnvDialogProps) {
  const [name, setName] = React.useState("");
  const [namespace, setNamespace] = React.useState("");

  // Sync form fields whenever the environment changes.
  React.useEffect(() => {
    if (environment) {
      setName(environment.name);
      setNamespace(environment.namespace);
    }
  }, [environment]);

  const updateMutation = useUpdateEnvironment(orgId, environment?.id ?? "", appId);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!environment || !name.trim() || !namespace.trim()) return;

    const data: UpdateEnvironmentRequest = {};
    if (name.trim() !== environment.name) data.name = name.trim();
    if (namespace.trim() !== environment.namespace) data.namespace = namespace.trim();
    if (Object.keys(data).length === 0) {
      onOpenChange(false);
      return;
    }

    await updateMutation.mutateAsync(data, {
      onSuccess: () => onOpenChange(false),
    });
  }

  return (
    <Dialog open={!!environment} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle>Edit Environment</DialogTitle>
          <DialogDescription>
            Update the name or Kubernetes namespace for this environment.
            Cluster cannot be changed after creation.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="edit-env-name">Name</Label>
            <Input
              id="edit-env-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="edit-env-namespace">Kubernetes Namespace</Label>
            <Input
              id="edit-env-namespace"
              value={namespace}
              onChange={(e) => setNamespace(e.target.value)}
            />
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                !name.trim() ||
                !namespace.trim() ||
                updateMutation.isPending
              }
            >
              {updateMutation.isPending && (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              )}
              Save Changes
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Environment Card
// ---------------------------------------------------------------------------

interface EnvironmentCardProps {
  env: Environment;
  clusterMap: Map<string, Cluster>;
  currentDeployment: Deployment | undefined;
  onEdit: (env: Environment) => void;
  deleteMutation: ReturnType<typeof useDeleteEnvironment>;
}

function EnvironmentCard({
  env,
  clusterMap,
  currentDeployment,
  onEdit,
  deleteMutation,
}: EnvironmentCardProps) {
  const cluster = clusterMap.get(env.clusterId);

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <Layers className="h-4 w-4 text-muted-foreground shrink-0" />
              <span className="truncate">{env.name}</span>
              {isProdName(env.name) && (
                <Badge variant="default" className="text-xs shrink-0">
                  Production
                </Badge>
              )}
            </CardTitle>
            <CardDescription className="mt-1 space-y-0.5">
              <div className="flex items-center gap-1 font-mono text-xs">
                <Server className="h-3 w-3" />
                {cluster ? cluster.name : env.clusterId.slice(0, 8)}
                {cluster && (
                  <Badge
                    variant={cluster.status === "connected" ? "outline" : "secondary"}
                    className="text-xs ml-1"
                  >
                    {cluster.status}
                  </Badge>
                )}
              </div>
              <div className="font-mono text-xs">ns: {env.namespace}</div>
            </CardDescription>
          </div>

          <div className="flex items-center gap-1 shrink-0">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => onEdit(env)}
              className="h-8 w-8 p-0"
              aria-label="Edit environment"
            >
              <Pencil className="h-3.5 w-3.5" />
            </Button>

            <ConfirmDialog
              trigger={
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 w-8 p-0 text-destructive hover:text-destructive"
                  aria-label="Delete environment"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              }
              title={`Delete "${env.name}"?`}
              description={`This will permanently delete the "${env.name}" environment. Any deployments bound to it will become unbound (their existing workloads are unaffected). This action cannot be undone.`}
              confirmLabel="Delete Environment"
              variant="destructive"
              onConfirm={() => deleteMutation.mutateAsync(env.id)}
              isLoading={deleteMutation.isPending}
            />
          </div>
        </div>
      </CardHeader>

      <CardContent className="pt-0">
        {/* Currently-deployed release / "No deployment yet" */}
        {currentDeployment ? (
          <div className="flex items-center gap-2 rounded-md bg-muted/50 px-3 py-2 text-sm">
            <PackageCheck className="h-4 w-4 text-green-600 shrink-0" />
            <div className="min-w-0">
              <span className="font-mono text-xs truncate block">
                {currentDeployment.image}
              </span>
              <div className="flex items-center gap-2 mt-0.5">
                <DeploymentStatusBadge status={currentDeployment.status} />
                {currentDeployment.currentRevision != null && (
                  <span className="text-xs text-muted-foreground">
                    rev {currentDeployment.currentRevision}
                  </span>
                )}
              </div>
            </div>
          </div>
        ) : (
          <div className="flex items-center gap-2 rounded-md border border-dashed px-3 py-2 text-sm text-muted-foreground">
            <Box className="h-4 w-4 shrink-0" />
            No deployment yet
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Tiny inline status badge (avoids importing the shared one which may differ)
// ---------------------------------------------------------------------------

function DeploymentStatusBadge({ status }: { status: string }) {
  const variantMap: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
    running: "default",
    pending: "secondary",
    succeeded: "outline",
    failed: "destructive",
    rolled_back: "secondary",
  };
  return (
    <Badge variant={variantMap[status] ?? "secondary"} className="text-xs capitalize">
      {status.replace("_", " ")}
    </Badge>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function ApplicationDetailPage() {
  const params = useParams();
  const router = useRouter();
  const orgSlug = params.orgSlug as string;
  const projectSlug = params.projectSlug as string;
  const appSlug = params.appSlug as string;

  const { organization } = useOrganization();
  const orgId = organization?.id ?? "";

  // --------------------------------------------------
  // Resolve project → app
  // --------------------------------------------------
  const {
    data: project,
    isLoading: isProjectLoading,
    error: projectError,
  } = useProjectBySlug(orgId, projectSlug);

  const projectId = project?.id ?? "";

  const { data: appsPage, isLoading: isAppsLoading } = useApplications(
    orgId,
    projectId
  );

  const application = React.useMemo(
    () => appsPage?.items.find((a) => a.slug === appSlug),
    [appsPage, appSlug]
  );

  const appId = application?.id ?? "";

  // --------------------------------------------------
  // Environments
  // --------------------------------------------------
  const {
    data: environments,
    isLoading: isEnvsLoading,
    error: envsError,
  } = useEnvironments(orgId, appId);

  // --------------------------------------------------
  // Clusters (for create form + display)
  // --------------------------------------------------
  const { data: clustersPage, isLoading: isClustersLoading } = useClusters(orgId);
  const clusters = clustersPage?.items ?? [];
  const clusterMap = React.useMemo(
    () => new Map(clusters.map((c) => [c.id, c])),
    [clusters]
  );

  // --------------------------------------------------
  // Deployments (to show "currently deployed release" per environment)
  // Uses the org-level endpoint filtered by applicationId; DeploymentView now
  // includes `environmentId` (added in Phase 1 backend work).
  // --------------------------------------------------
  const { data: deploymentsPage } = useQuery({
    queryKey: ["organizations", orgId, "deployments", { applicationId: appId }],
    queryFn: () => deploymentsApi.list(orgId, { applicationId: appId }),
    enabled: !!orgId && !!appId,
  });

  /**
   * Build a map from environmentId → the most-recently-created deployment
   * bound to that environment.  Multiple deployments can share an environmentId
   * (each ApprovePromotion creates a new one), so we keep the latest by createdAt.
   */
  const deploymentByEnvId = React.useMemo(() => {
    const map = new Map<string, Deployment>();
    for (const dep of deploymentsPage?.items ?? []) {
      if (!dep.environmentId) continue;
      const existing = map.get(dep.environmentId);
      if (!existing || dep.createdAt > existing.createdAt) {
        map.set(dep.environmentId, dep);
      }
    }
    return map;
  }, [deploymentsPage]);

  // --------------------------------------------------
  // Dialog state
  // --------------------------------------------------
  const [createOpen, setCreateOpen] = React.useState(false);
  const [editingEnv, setEditingEnv] = React.useState<Environment | null>(null);

  const deleteMutation = useDeleteEnvironment(orgId, appId);

  // --------------------------------------------------
  // Loading / error states
  // --------------------------------------------------
  if (isProjectLoading || isAppsLoading) {
    return <LoadingState message="Loading application…" />;
  }

  if (projectError) {
    return <ErrorState title="Project not found" />;
  }

  if (!application && !isAppsLoading) {
    return (
      <ErrorState
        title="Application not found"
        message={`No application with slug "${appSlug}" in this project.`}
      />
    );
  }

  const isLoading = isEnvsLoading || isClustersLoading;

  return (
    <div className="space-y-6">
      {/* Back nav */}
      <button
        onClick={() => router.push(`/${orgSlug}/projects/${projectSlug}`)}
        className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to {project?.name ?? projectSlug}
      </button>

      <PageHeader
        title={application?.name ?? appSlug}
        description={application?.description || "Manage environments and deployments"}
      />

      {/* Environments section */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold">Environments</h2>
            <p className="text-sm text-muted-foreground">
              Each environment binds this application to a specific cluster and
              Kubernetes namespace.
            </p>
          </div>
          <Button onClick={() => setCreateOpen(true)} disabled={isClustersLoading}>
            <Plus className="mr-2 h-4 w-4" />
            Add Environment
          </Button>
        </div>

        {isLoading ? (
          <LoadingState message="Loading environments…" />
        ) : envsError ? (
          <ErrorState
            title="Failed to load environments"
            message={(envsError as Error).message}
          />
        ) : !environments || environments.length === 0 ? (
          <EmptyState
            icon={<Layers className="h-6 w-6" />}
            title="No environments yet"
            description="Create an environment to define where this application is deployed."
            action={
              <Button onClick={() => setCreateOpen(true)}>
                <Plus className="mr-2 h-4 w-4" />
                Add Environment
              </Button>
            }
          />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {environments.map((env) => (
              <EnvironmentCard
                key={env.id}
                env={env}
                clusterMap={clusterMap}
                currentDeployment={deploymentByEnvId.get(env.id)}
                onEdit={setEditingEnv}
                deleteMutation={deleteMutation}
              />
            ))}
          </div>
        )}
      </div>

      {/* Dialogs */}
      {application && (
        <>
          <CreateEnvironmentDialog
            open={createOpen}
            onOpenChange={setCreateOpen}
            orgId={orgId}
            appId={appId}
            existingEnvs={environments ?? []}
            clusters={clusters}
          />

          <EditEnvironmentDialog
            environment={editingEnv}
            onOpenChange={(open) => {
              if (!open) setEditingEnv(null);
            }}
            orgId={orgId}
            appId={appId}
          />
        </>
      )}
    </div>
  );
}
