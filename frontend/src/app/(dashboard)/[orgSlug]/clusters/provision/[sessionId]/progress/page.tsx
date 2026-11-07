"use client";

import * as React from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import {
  ArrowLeft,
  Check,
  Circle,
  Clock,
  Copy,
  ExternalLink,
  Loader2,
  AlertCircle,
  Server,
  Terminal,
  XCircle,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { useOrganization } from "@/providers/organization-provider";
import { provisioningApi } from "@/lib/api";
import type {
  InstallSessionView,
  StepView,
  StepInfo,
} from "@/lib/api/provisioning";
import { SessionStatus, StepStatus } from "@/lib/api/provisioning";
import { useQuery } from "@tanstack/react-query";
import { toast } from "@/hooks/use-toast";
import { cn } from "@/lib/utils";

export default function ProvisioningProgressPage() {
  const params = useParams();
  const router = useRouter();
  const orgSlug = params.orgSlug as string;
  const sessionId = params.sessionId as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";
  const [copied, setCopied] = React.useState(false);

  // Poll for session status
  const {
    data: sessionData,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ["installSession", orgId, sessionId],
    queryFn: () => provisioningApi.getInstallSession(orgId, sessionId),
    enabled: !!orgId && !!sessionId,
    refetchInterval: (query) => {
      // Poll every 5 seconds while active
      const session = query.state.data?.session;
      if (
        session &&
        (session.status === SessionStatus.ACTIVE ||
          session.status === SessionStatus.COMPLETED ||
          session.status === SessionStatus.FAILED)
      ) {
        // Continue polling for a while even after completion to catch final updates
        if (session.status === SessionStatus.ACTIVE) {
          return 5000;
        }
        // After completion/failure, poll a few more times then stop
        return false;
      }
      return 5000;
    },
  });

  const session = sessionData?.session;
  const steps = sessionData?.steps || [];

  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      toast({ title: "Copied to clipboard" });
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      toast({ title: "Failed to copy", variant: "destructive" });
    }
  };

  const getStepIcon = (status: string) => {
    switch (status) {
      case StepStatus.COMPLETED:
        return <Check className="h-5 w-5 text-green-500" />;
      case StepStatus.RUNNING:
        return <Loader2 className="h-5 w-5 text-primary animate-spin" />;
      case StepStatus.FAILED:
        return <XCircle className="h-5 w-5 text-destructive" />;
      case StepStatus.SKIPPED:
        return <Circle className="h-5 w-5 text-muted-foreground" />;
      default:
        return <Clock className="h-5 w-5 text-muted-foreground" />;
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case SessionStatus.COMPLETED:
        return "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200";
      case SessionStatus.FAILED:
        return "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200";
      case SessionStatus.ACTIVE:
        return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200";
      case SessionStatus.EXPIRED:
        return "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200";
      default:
        return "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200";
    }
  };

  const formatDuration = (ms?: number) => {
    if (!ms) return "-";
    if (ms < 1000) return `${ms}ms`;
    const seconds = Math.floor(ms / 1000);
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const remainingSeconds = seconds % 60;
    return `${minutes}m ${remainingSeconds}s`;
  };

  if (!organization) {
    return <LoadingState />;
  }

  if (isLoading) {
    return <LoadingState message="Loading provisioning status..." />;
  }

  if (error) {
    return <ErrorState onRetry={() => refetch()} />;
  }

  if (!session) {
    return <ErrorState message="Session not found" onRetry={() => refetch()} />;
  }

  const isComplete = session.status === SessionStatus.COMPLETED;
  const isFailed = session.status === SessionStatus.FAILED;
  const progressPercent = Math.round(
    (session.completedSteps / session.totalSteps) * 100
  );

  return (
    <div className="space-y-6">
      <PageHeader
        title="Provisioning Progress"
        description={`Tracking cluster creation: ${session.currentStep || "Initializing"}`}
        action={
          <Link href={`/${orgSlug}/clusters`}>
            <Button variant="outline">
              <ArrowLeft className="mr-2 h-4 w-4" />
              Back to Clusters
            </Button>
          </Link>
        }
      />

      {/* Status Overview */}
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-3">
                <Server className="h-5 w-5" />
                Installation Progress
              </CardTitle>
              <CardDescription>
                Session ID: {session.id}
              </CardDescription>
            </div>
            <Badge className={getStatusColor(session.status)}>
              {session.status}
            </Badge>
          </div>
        </CardHeader>
        <CardContent>
          {/* Progress Bar */}
          <div className="space-y-2">
            <div className="flex justify-between text-sm">
              <span>
                Step {session.completedSteps} of {session.totalSteps}
              </span>
              <span>{progressPercent}%</span>
            </div>
            <div className="h-2 bg-muted rounded-full overflow-hidden">
              <div
                className={cn(
                  "h-full transition-all duration-500",
                  isFailed ? "bg-destructive" : "bg-primary"
                )}
                style={{ width: `${progressPercent}%` }}
              />
            </div>
          </div>

          {/* Success Message */}
          {isComplete && session.agentConnected && (
            <div className="mt-4 p-4 bg-green-50 border border-green-200 rounded-lg dark:bg-green-950 dark:border-green-800">
              <div className="flex items-start gap-3">
                <Check className="h-5 w-5 text-green-600 mt-0.5" />
                <div>
                  <p className="font-medium text-green-800 dark:text-green-200">
                    Cluster provisioned successfully!
                  </p>
                  <p className="text-sm text-green-700 dark:text-green-300 mt-1">
                    The agent is connected and your cluster is ready to use.
                    {session.agentVersion && (
                      <span> Agent version: {session.agentVersion}</span>
                    )}
                  </p>
                  <Link href={`/${orgSlug}/clusters`}>
                    <Button size="sm" className="mt-3">
                      Go to Clusters
                      <ExternalLink className="ml-2 h-4 w-4" />
                    </Button>
                  </Link>
                </div>
              </div>
            </div>
          )}

          {/* Failure Message */}
          {isFailed && (
            <div className="mt-4 p-4 bg-red-50 border border-red-200 rounded-lg dark:bg-red-950 dark:border-red-800">
              <div className="flex items-start gap-3">
                <XCircle className="h-5 w-5 text-red-600 mt-0.5" />
                <div>
                  <p className="font-medium text-red-800 dark:text-red-200">
                    Provisioning failed
                  </p>
                  <p className="text-sm text-red-700 dark:text-red-300 mt-1">
                    Check the step details below for error information.
                  </p>
                </div>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Install Instructions */}
      {session.status === SessionStatus.ACTIVE && !session.agentConnected && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Terminal className="h-5 w-5" />
              Installation Instructions
            </CardTitle>
            <CardDescription>
              Run the following command on a machine with Terraform, kubectl, and your cloud CLI
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Prerequisites */}
            <div className="p-4 bg-blue-50 border border-blue-200 rounded-lg dark:bg-blue-950 dark:border-blue-800">
              <div className="flex items-start gap-3">
                <AlertCircle className="h-5 w-5 text-blue-600 mt-0.5" />
                <div>
                  <p className="font-medium text-blue-800 dark:text-blue-200">
                    Prerequisites
                  </p>
                  <p className="text-sm text-blue-700 dark:text-blue-300 mt-1">
                    Ensure you have the following tools installed:
                  </p>
                  <ul className="list-disc list-inside text-sm text-blue-700 dark:text-blue-300 mt-2 space-y-1">
                    <li><code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">curl</code> and <code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">jq</code></li>
                    <li><code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">terraform</code> or <code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">tofu</code></li>
                    <li><code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">kubectl</code></li>
                    <li>Your cloud provider CLI (<code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">aws</code>, <code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">gcloud</code>, or <code className="bg-blue-100 dark:bg-blue-900 px-1 rounded">az</code>)</li>
                  </ul>
                </div>
              </div>
            </div>

            {/* One-liner Install Command */}
            <div className="space-y-2">
              <label className="text-sm font-medium">Install Command</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 p-3 bg-slate-950 text-slate-50 rounded-lg text-sm font-mono overflow-x-auto whitespace-nowrap">
                  curl -sSL {process.env.NEXT_PUBLIC_API_BASE_URL || ""}/v1/install/{session.id} | PLATFORM_INSTALL_TOKEN={session.sessionToken} sh
                </code>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="outline"
                        size="icon"
                        onClick={() =>
                          copyToClipboard(
                            `curl -sSL ${process.env.NEXT_PUBLIC_API_BASE_URL || ""}/v1/install/${session.id} | PLATFORM_INSTALL_TOKEN=${session.sessionToken} sh`
                          )
                        }
                      >
                        {copied ? (
                          <Check className="h-4 w-4" />
                        ) : (
                          <Copy className="h-4 w-4" />
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Copy command</TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>
              <p className="text-xs text-muted-foreground">
                This command will provision the cluster infrastructure and install the BDS Platform agent.
                The process may take 10-20 minutes depending on your cloud provider.
              </p>
            </div>

            {/* What the script does */}
            <div className="space-y-2">
              <label className="text-sm font-medium">What happens when you run this?</label>
              <ol className="list-decimal list-inside text-sm text-muted-foreground space-y-1">
                <li>Validates prerequisites (terraform, kubectl, cloud CLI)</li>
                <li>Fetches Terraform configuration from the platform</li>
                <li>Runs <code className="bg-muted px-1 rounded">terraform init</code> and <code className="bg-muted px-1 rounded">terraform apply</code></li>
                <li>Configures <code className="bg-muted px-1 rounded">kubectl</code> access to the new cluster</li>
                <li>Installs the BDS Platform agent</li>
                <li>Waits for agent to register with the platform</li>
              </ol>
            </div>

            {/* Alternative: Manual Steps */}
            <details className="text-sm">
              <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
                Alternative: Manual installation steps
              </summary>
              <div className="mt-3 space-y-3 pl-4 border-l-2 border-muted">
                <div className="space-y-1">
                  <label className="text-xs font-medium">Session Token</label>
                  <code className="block p-2 bg-muted rounded text-xs font-mono break-all">
                    {session.sessionToken}
                  </code>
                </div>
                <div className="space-y-1">
                  <label className="text-xs font-medium">Session Bundle API</label>
                  <code className="block p-2 bg-muted rounded text-xs font-mono">
                    curl -s {process.env.NEXT_PUBLIC_API_BASE_URL || ""}/v1/sessions/{session.sessionToken}/bundle
                  </code>
                </div>
                {session.bootstrapCommand && (
                  <div className="space-y-1">
                    <label className="text-xs font-medium">Agent Installation (after Terraform)</label>
                    <code className="block p-2 bg-muted rounded text-xs font-mono break-all">
                      {session.bootstrapCommand}
                    </code>
                  </div>
                )}
              </div>
            </details>

            {/* Token Expiry */}
            <p className="text-sm text-muted-foreground">
              This session expires at:{" "}
              <span className="font-medium">
                {new Date(session.expiresAt).toLocaleString()}
              </span>
            </p>
          </CardContent>
        </Card>
      )}

      {/* Steps Timeline */}
      <Card>
        <CardHeader>
          <CardTitle>Installation Steps</CardTitle>
          <CardDescription>
            Detailed progress of each installation step
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {/* Use session.steps if detailed steps not available */}
            {(steps.length > 0 ? steps : session.steps).map((step, index) => {
              const isStepView = "id" in step;
              const stepStatus = isStepView
                ? (step as StepView).status
                : (step as StepInfo).status;
              const stepName = isStepView
                ? (step as StepView).name
                : (step as StepInfo).name;
              const stepDescription = isStepView
                ? (step as StepView).description
                : (step as StepInfo).description;
              const stepOutput = isStepView ? (step as StepView).output : undefined;
              const stepError = isStepView ? (step as StepView).error : undefined;
              const stepDuration = isStepView
                ? (step as StepView).durationMs
                : undefined;

              return (
                <div
                  key={index}
                  className={cn(
                    "flex items-start gap-4 p-4 rounded-lg border transition-colors",
                    stepStatus === StepStatus.RUNNING && "bg-primary/5 border-primary/20",
                    stepStatus === StepStatus.COMPLETED && "bg-muted/50",
                    stepStatus === StepStatus.FAILED && "bg-destructive/5 border-destructive/20"
                  )}
                >
                  <div className="flex-shrink-0 mt-0.5">
                    {getStepIcon(stepStatus)}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between">
                      <div>
                        <p className="font-medium">{formatStepName(stepName)}</p>
                        {stepDescription && (
                          <p className="text-sm text-muted-foreground">
                            {stepDescription}
                          </p>
                        )}
                      </div>
                      {stepDuration !== undefined && stepDuration > 0 && (
                        <span className="text-sm text-muted-foreground">
                          {formatDuration(stepDuration)}
                        </span>
                      )}
                    </div>
                    {stepError && (
                      <div className="mt-2 p-2 bg-destructive/10 rounded text-sm text-destructive font-mono">
                        {stepError}
                      </div>
                    )}
                    {stepOutput && (
                      <details className="mt-2">
                        <summary className="text-sm text-muted-foreground cursor-pointer hover:text-foreground">
                          View output
                        </summary>
                        <pre className="mt-2 p-2 bg-muted rounded text-xs font-mono overflow-x-auto max-h-40">
                          {stepOutput}
                        </pre>
                      </details>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function formatStepName(name: string): string {
  return name
    .split("_")
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}
