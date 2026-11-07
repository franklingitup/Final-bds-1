"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { Plus, Box, Key, Rocket, GitBranch, Loader2, Trash2, Play, XCircle, RotateCcw, FileText, ChevronDown, ChevronRight, Github, RefreshCw, Link2, Webhook, ExternalLink } from "lucide-react";
import Link from "next/link";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useProjectBySlug } from "@/hooks/use-projects";
import { useApplications } from "@/hooks/use-applications";
import { useSecrets } from "@/hooks/use-secrets";
import { useDeployments } from "@/hooks/use-deployments";
import { useOrganization } from "@/providers/organization-provider";
import { formatRelativeTime } from "@/lib/utils";
import { CreateApplicationDialog } from "@/components/applications/create-application-dialog";
import { CreateSecretDialog } from "@/components/secrets/create-secret-dialog";
import { ApplicationCard } from "@/components/applications/application-card";
import { SecretsList } from "@/components/secrets/secrets-list";
import { DeploymentsList } from "@/components/deployments/deployments-list";
import { buildsApi, type RepositoryView, type BuildView, type BuildLogView, BuildStatus, AuthType } from "@/lib/api/builds";
import { githubApi, type GitHubRepoItem } from "@/lib/api/github";
import { toast } from "@/hooks/use-toast";

// Build status badge
function BuildStatusBadge({ status }: { status: string }) {
  const variants: Record<string, { variant: "default" | "secondary" | "destructive" | "outline"; label: string }> = {
    [BuildStatus.SUCCEEDED]: { variant: "default", label: "Succeeded" },
    [BuildStatus.QUEUED]: { variant: "secondary", label: "Queued" },
    [BuildStatus.CLONING]: { variant: "outline", label: "Cloning" },
    [BuildStatus.BUILDING]: { variant: "outline", label: "Building" },
    [BuildStatus.PUSHING]: { variant: "outline", label: "Pushing" },
    [BuildStatus.FAILED]: { variant: "destructive", label: "Failed" },
    [BuildStatus.CANCELLED]: { variant: "secondary", label: "Cancelled" },
  };
  const config = variants[status] || { variant: "secondary" as const, label: status };
  return <Badge variant={config.variant}>{config.label}</Badge>;
}

// Build is in progress
function isBuildInProgress(status: string): boolean {
  return [BuildStatus.QUEUED, BuildStatus.CLONING, BuildStatus.BUILDING, BuildStatus.PUSHING].includes(status as any);
}

export default function ProjectDetailPage() {
  const params = useParams();
  const projectSlug = params.projectSlug as string;
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";
  const queryClient = useQueryClient();

  const { data: project, isLoading: isProjectLoading, error: projectError, refetch: refetchProject } = 
    useProjectBySlug(orgId, projectSlug);
  
  const projectId = project?.id || "";
  
  const { data: apps, isLoading: isAppsLoading } = useApplications(orgId, projectId);
  const { data: secrets, isLoading: isSecretsLoading } = useSecrets(orgId, projectId);
  const { data: deployments, isLoading: isDeploymentsLoading } = useDeployments(orgId, { applicationId: projectId });

  const [createAppOpen, setCreateAppOpen] = React.useState(false);
  const [createSecretOpen, setCreateSecretOpen] = React.useState(false);
  const [activeTab, setActiveTab] = React.useState("applications");
  
  // Repository state
  const [newRepoUrl, setNewRepoUrl] = React.useState("");
  const [newRepoName, setNewRepoName] = React.useState("");
  const [newRepoBranch, setNewRepoBranch] = React.useState("main");
  const [newRepoAuthType, setNewRepoAuthType] = React.useState<string>(AuthType.NONE);
  const [newRepoAuthSecret, setNewRepoAuthSecret] = React.useState<string>("");
  const [expandedRepo, setExpandedRepo] = React.useState<string | null>(null);
  const [viewingLogsForBuild, setViewingLogsForBuild] = React.useState<string | null>(null);
  
  // GitHub repo selection state
  const [selectedConnection, setSelectedConnection] = React.useState<string | null>(null);
  const [githubRepoSearch, setGithubRepoSearch] = React.useState("");
  
  // Repositories query
  const { data: repositories, isLoading: isReposLoading } = useQuery({
    queryKey: ["repositories", orgId, projectId],
    queryFn: () => buildsApi.listRepositories(orgId, projectId),
    enabled: !!orgId && !!projectId && activeTab === "repositories",
  });
  
  // GitHub connections query
  const { data: githubConnections, isLoading: isConnectionsLoading } = useQuery({
    queryKey: ["github-connections", orgId],
    queryFn: () => githubApi.listConnections(orgId),
    enabled: !!orgId && activeTab === "repositories",
  });
  
  // GitHub user repos query (when a connection is selected)
  const { data: githubUserRepos, isLoading: isGithubReposLoading, refetch: refetchGithubRepos } = useQuery({
    queryKey: ["github-user-repos", orgId, selectedConnection],
    queryFn: () => githubApi.listUserGitHubRepositories(orgId, selectedConnection!, { perPage: 100 }),
    enabled: !!orgId && !!selectedConnection,
  });
  
  // Connected GitHub repos query
  const { data: connectedGithubRepos, isLoading: isConnectedReposLoading } = useQuery({
    queryKey: ["github-connected-repos", orgId],
    queryFn: () => githubApi.listRepositories(orgId),
    enabled: !!orgId && activeTab === "repositories",
  });
  
  // Builds query (for expanded repo)
  const { data: builds, isLoading: isBuildsLoading } = useQuery({
    queryKey: ["builds", orgId, expandedRepo],
    queryFn: () => buildsApi.listBuilds(orgId, { repositoryId: expandedRepo! }),
    enabled: !!orgId && !!expandedRepo,
    refetchInterval: expandedRepo ? 5000 : false, // Poll while viewing builds
  });
  
  // Build logs query
  const { data: buildLogs, isLoading: isLogsLoading } = useQuery({
    queryKey: ["buildLogs", orgId, viewingLogsForBuild],
    queryFn: () => buildsApi.getBuildLogs(orgId, viewingLogsForBuild!),
    enabled: !!orgId && !!viewingLogsForBuild,
  });
  
  // Repository mutations
  const createRepository = useMutation({
    mutationFn: () => buildsApi.createRepository(orgId, projectId, {
      name: newRepoName,
      url: newRepoUrl,
      defaultBranch: newRepoBranch || "main",
      authType: newRepoAuthType,
      authSecretId: newRepoAuthSecret || undefined,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["repositories", orgId, projectId] });
      setNewRepoUrl("");
      setNewRepoName("");
      setNewRepoBranch("main");
      setNewRepoAuthType(AuthType.NONE);
      setNewRepoAuthSecret("");
      toast({ title: "Repository connected" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to connect repository", description: error.message, variant: "destructive" });
    },
  });
  
  const deleteRepository = useMutation({
    mutationFn: (repoId: string) => buildsApi.deleteRepository(orgId, repoId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["repositories", orgId, projectId] });
      setExpandedRepo(null);
      toast({ title: "Repository disconnected" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to disconnect repository", description: error.message, variant: "destructive" });
    },
  });
  
  // Build mutations
  const triggerBuild = useMutation({
    mutationFn: (repo: RepositoryView) => buildsApi.createBuild(orgId, {
      repositoryId: repo.id,
      gitRef: repo.defaultBranch,
      targetImage: `${repo.name.toLowerCase()}:latest`,
      targetRegistry: "registry.bdsplatform.io",
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["builds", orgId, expandedRepo] });
      toast({ title: "Build started" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to start build", description: error.message, variant: "destructive" });
    },
  });
  
  const cancelBuild = useMutation({
    mutationFn: (buildId: string) => buildsApi.cancelBuild(orgId, buildId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["builds", orgId, expandedRepo] });
      toast({ title: "Build cancelled" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to cancel build", description: error.message, variant: "destructive" });
    },
  });
  
  const retryBuild = useMutation({
    mutationFn: (buildId: string) => buildsApi.retryBuild(orgId, buildId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["builds", orgId, expandedRepo] });
      toast({ title: "Build restarted" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to retry build", description: error.message, variant: "destructive" });
    },
  });
  
  const connectGithubRepo = useMutation({
    mutationFn: (repo: GitHubRepoItem) => githubApi.connectRepository(orgId, {
      connectionId: selectedConnection!,
      owner: repo.owner,
      repo: repo.name,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["github-connected-repos", orgId] });
      toast({ title: "Repository connected" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to connect repository", description: error.message, variant: "destructive" });
    },
  });
  
  const deleteGithubRepo = useMutation({
    mutationFn: (repoId: string) => githubApi.deleteRepository(orgId, repoId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["github-connected-repos", orgId] });
      toast({ title: "Repository disconnected" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to disconnect repository", description: error.message, variant: "destructive" });
    },
  });
  
  const registerWebhook = useMutation({
    mutationFn: (repoId: string) => githubApi.registerWebhook(orgId, repoId, { events: ["push", "pull_request"] }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["github-connected-repos", orgId] });
      toast({ title: "Webhook registered", description: "Pushes will now trigger builds automatically" });
    },
    onError: (error: Error) => {
      toast({ title: "Failed to register webhook", description: error.message, variant: "destructive" });
    },
  });
  
  const filteredGithubRepos = React.useMemo(() => {
    if (!githubUserRepos?.items) return [];
    if (!githubRepoSearch) return githubUserRepos.items;
    const search = githubRepoSearch.toLowerCase();
    return githubUserRepos.items.filter(r => 
      r.name.toLowerCase().includes(search) || 
      r.fullName.toLowerCase().includes(search)
    );
  }, [githubUserRepos, githubRepoSearch]);

  if (isProjectLoading) {
    return <LoadingState message="Loading project..." />;
  }

  if (projectError) {
    return <ErrorState onRetry={() => refetchProject()} />;
  }

  if (!project) {
    return <ErrorState title="Project not found" message="The project you're looking for doesn't exist." />;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={project.name}
        description={project.description || "No description"}
      />

      <Tabs defaultValue="applications" value={activeTab} onValueChange={setActiveTab} className="space-y-6">
        <TabsList>
          <TabsTrigger value="applications" className="gap-2">
            <Box className="h-4 w-4" />
            Applications
          </TabsTrigger>
          <TabsTrigger value="repositories" className="gap-2">
            <GitBranch className="h-4 w-4" />
            Repositories
          </TabsTrigger>
          <TabsTrigger value="secrets" className="gap-2">
            <Key className="h-4 w-4" />
            Secrets
          </TabsTrigger>
          <TabsTrigger value="deployments" className="gap-2">
            <Rocket className="h-4 w-4" />
            Deployments
          </TabsTrigger>
        </TabsList>

        <TabsContent value="applications" className="space-y-4">
          <div className="flex justify-end">
            <Button onClick={() => setCreateAppOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              New Application
            </Button>
          </div>

          {isAppsLoading ? (
            <LoadingState message="Loading applications..." />
          ) : apps?.items.length === 0 ? (
            <EmptyState
              icon={<Box className="h-6 w-6" />}
              title="No applications yet"
              description="Create an application to start deploying"
              action={
                <Button onClick={() => setCreateAppOpen(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  Create Application
                </Button>
              }
            />
          ) : (
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              {apps?.items.map((app) => (
                <ApplicationCard
                  key={app.id}
                  application={app}
                  orgSlug={orgSlug}
                  projectSlug={projectSlug}
                />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="repositories" className="space-y-4">
          {/* GitHub Repository Picker (connection management lives at /integrations) */}
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle className="flex items-center gap-2">
                    <Github className="h-5 w-5" />
                    Import from GitHub
                  </CardTitle>
                  <CardDescription>
                    Select a repository from your connected GitHub accounts
                  </CardDescription>
                </div>
                <Link href={`/${orgSlug}/integrations`}>
                  <Button variant="outline" size="sm">
                    <ExternalLink className="mr-2 h-4 w-4" />
                    Manage connections
                  </Button>
                </Link>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              {isConnectionsLoading ? (
                <div className="flex items-center justify-center py-4">
                  <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
              ) : githubConnections?.items && githubConnections.items.length > 0 ? (
                <>
                  {/* Account selector */}
                  <div className="flex flex-wrap gap-2">
                    {githubConnections.items.map((conn) => (
                      <Button
                        key={conn.id}
                        variant={selectedConnection === conn.id ? "default" : "outline"}
                        size="sm"
                        onClick={() =>
                          setSelectedConnection(selectedConnection === conn.id ? null : conn.id)
                        }
                        className="gap-2"
                      >
                        {conn.githubAvatar ? (
                          <img src={conn.githubAvatar} alt="" className="h-4 w-4 rounded-full" />
                        ) : (
                          <Github className="h-4 w-4" />
                        )}
                        {conn.githubUsername ?? conn.name}
                      </Button>
                    ))}
                  </div>

                  {/* Repository picker for selected account */}
                  {selectedConnection && (
                    <div>
                      <div className="flex items-center justify-between mb-2">
                        <Label>Select repository</Label>
                        <Button variant="ghost" size="sm" onClick={() => refetchGithubRepos()}>
                          <RefreshCw className="h-4 w-4" />
                        </Button>
                      </div>
                      <Input
                        placeholder="Search repositories..."
                        value={githubRepoSearch}
                        onChange={(e) => setGithubRepoSearch(e.target.value)}
                        className="mb-2"
                      />
                      <div className="max-h-[280px] overflow-auto rounded-lg border">
                        {isGithubReposLoading ? (
                          <div className="flex items-center justify-center py-8">
                            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                          </div>
                        ) : filteredGithubRepos.length > 0 ? (
                          <div className="divide-y">
                            {filteredGithubRepos.map((repo) => (
                              <div
                                key={repo.id}
                                className="flex items-center justify-between p-3 hover:bg-muted/50"
                              >
                                <div className="min-w-0 flex-1">
                                  <p className="font-medium truncate">{repo.fullName}</p>
                                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                                    {repo.isPrivate && (
                                      <Badge variant="secondary" className="text-xs">
                                        Private
                                      </Badge>
                                    )}
                                    {repo.language && <span>{repo.language}</span>}
                                    <span>⭐ {repo.starsCount}</span>
                                  </div>
                                </div>
                                <Button
                                  size="sm"
                                  onClick={() => connectGithubRepo.mutate(repo)}
                                  disabled={connectGithubRepo.isPending}
                                >
                                  {connectGithubRepo.isPending ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                  ) : (
                                    <Link2 className="h-4 w-4" />
                                  )}
                                  <span className="ml-1">Connect</span>
                                </Button>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <p className="text-sm text-muted-foreground text-center py-8">
                            No repositories found
                          </p>
                        )}
                      </div>
                    </div>
                  )}
                </>
              ) : (
                <div className="flex flex-col items-center gap-2 py-6 text-center">
                  <Github className="h-8 w-8 text-muted-foreground" />
                  <p className="text-sm text-muted-foreground">No GitHub accounts connected yet.</p>
                  <Link href={`/${orgSlug}/integrations`}>
                    <Button size="sm" variant="outline">
                      <ExternalLink className="mr-2 h-4 w-4" />
                      Connect GitHub in Integrations
                    </Button>
                  </Link>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Connected GitHub Repos */}
          {connectedGithubRepos?.items && connectedGithubRepos.items.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>Connected GitHub Repositories</CardTitle>
                <CardDescription>
                  Repositories linked via GitHub integration
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Repository</TableHead>
                      <TableHead>Branch</TableHead>
                      <TableHead>Webhook</TableHead>
                      <TableHead className="w-[150px]">Actions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {connectedGithubRepos.items.map((repo) => (
                      <TableRow key={repo.id}>
                        <TableCell>
                          <a
                            href={repo.htmlUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="font-medium hover:underline"
                          >
                            {repo.fullName}
                          </a>
                          {repo.isPrivate && (
                            <Badge variant="secondary" className="ml-2 text-xs">Private</Badge>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline">{repo.defaultBranch}</Badge>
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => registerWebhook.mutate(repo.id)}
                            disabled={registerWebhook.isPending}
                          >
                            {registerWebhook.isPending ? (
                              <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                            ) : (
                              <Webhook className="mr-1 h-4 w-4" />
                            )}
                            Setup Webhook
                          </Button>
                        </TableCell>
                        <TableCell>
                          <ConfirmDialog
                            trigger={
                              <Button variant="ghost" size="sm" className="text-destructive">
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            }
                            title="Disconnect Repository"
                            description={`Disconnect ${repo.fullName}?`}
                            confirmLabel="Disconnect"
                            variant="destructive"
                            onConfirm={() => deleteGithubRepo.mutate(repo.id)}
                          />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}

          {/* Manual Git URL Form (fallback) */}
          <Card>
            <CardHeader>
              <CardTitle>Connect Repository Manually</CardTitle>
              <CardDescription>
                Connect a Git repository using URL (for non-GitHub or self-hosted repos)
              </CardDescription>
            </CardHeader>
            <CardContent>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  if (newRepoUrl.trim() && newRepoName.trim()) {
                    createRepository.mutate();
                  }
                }}
                className="space-y-4"
              >
                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <Label htmlFor="repo-name">Repository Name</Label>
                    <Input
                      id="repo-name"
                      placeholder="my-app"
                      value={newRepoName}
                      onChange={(e) => setNewRepoName(e.target.value)}
                      disabled={createRepository.isPending}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="repo-url">Git URL</Label>
                    <Input
                      id="repo-url"
                      placeholder="https://github.com/org/repo.git"
                      value={newRepoUrl}
                      onChange={(e) => setNewRepoUrl(e.target.value)}
                      disabled={createRepository.isPending}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="repo-branch">Default Branch</Label>
                    <Input
                      id="repo-branch"
                      placeholder="main"
                      value={newRepoBranch}
                      onChange={(e) => setNewRepoBranch(e.target.value)}
                      disabled={createRepository.isPending}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="repo-auth">Authentication</Label>
                    <Select value={newRepoAuthType} onValueChange={setNewRepoAuthType}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={AuthType.NONE}>None (Public Repo)</SelectItem>
                        <SelectItem value={AuthType.TOKEN}>Access Token</SelectItem>
                        <SelectItem value={AuthType.SSH_KEY}>SSH Key</SelectItem>
                        <SelectItem value={AuthType.DEPLOY_KEY}>Deploy Key</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  {newRepoAuthType !== AuthType.NONE && (
                    <div className="space-y-2 md:col-span-2">
                      <Label htmlFor="repo-secret">Auth Secret</Label>
                      <Select value={newRepoAuthSecret} onValueChange={setNewRepoAuthSecret}>
                        <SelectTrigger>
                          <SelectValue placeholder="Select a secret..." />
                        </SelectTrigger>
                        <SelectContent>
                          {secrets?.items.map((secret) => (
                            <SelectItem key={secret.id} value={secret.id}>
                              {secret.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-muted-foreground">
                        Select a secret from this project to use for authentication
                      </p>
                    </div>
                  )}
                </div>
                <Button 
                  type="submit" 
                  disabled={!newRepoUrl.trim() || !newRepoName.trim() || createRepository.isPending}
                >
                  {createRepository.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                  <GitBranch className="mr-2 h-4 w-4" />
                  Connect Repository
                </Button>
              </form>
            </CardContent>
          </Card>

          {/* Repositories List */}
          {isReposLoading ? (
            <LoadingState message="Loading repositories..." />
          ) : repositories?.items.length === 0 ? (
            <EmptyState
              icon={<GitBranch className="h-6 w-6" />}
              title="No repositories connected"
              description="Connect a Git repository to enable automated builds"
            />
          ) : (
            <div className="space-y-4">
              {repositories?.items.map((repo) => (
                <Card key={repo.id}>
                  <CardHeader className="pb-2">
                    <div className="flex items-center justify-between">
                      <Button
                        variant="ghost"
                        className="p-0 h-auto hover:bg-transparent"
                        onClick={() => {
                          setExpandedRepo(expandedRepo === repo.id ? null : repo.id);
                          setViewingLogsForBuild(null);
                        }}
                      >
                        <div className="flex items-center gap-2">
                          {expandedRepo === repo.id ? (
                            <ChevronDown className="h-4 w-4" />
                          ) : (
                            <ChevronRight className="h-4 w-4" />
                          )}
                          <GitBranch className="h-4 w-4 text-muted-foreground" />
                          <span className="font-medium">{repo.name}</span>
                          <Badge variant="outline" className="text-xs">{repo.defaultBranch}</Badge>
                        </div>
                      </Button>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            setExpandedRepo(repo.id);
                            triggerBuild.mutate(repo);
                          }}
                          disabled={triggerBuild.isPending}
                        >
                          {triggerBuild.isPending ? (
                            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                          ) : (
                            <Play className="mr-2 h-4 w-4" />
                          )}
                          Build
                        </Button>
                        <ConfirmDialog
                          trigger={
                            <Button variant="ghost" size="sm" className="text-destructive">
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          }
                          title="Disconnect Repository"
                          description={`Disconnect ${repo.name}? Existing builds will be preserved.`}
                          confirmLabel="Disconnect"
                          variant="destructive"
                          onConfirm={() => deleteRepository.mutate(repo.id)}
                        />
                      </div>
                    </div>
                    <p className="text-sm text-muted-foreground font-mono">{repo.url}</p>
                  </CardHeader>
                  
                  {expandedRepo === repo.id && (
                    <CardContent className="pt-4 border-t">
                      {/* Build logs viewer */}
                      {viewingLogsForBuild && (
                        <div className="mb-4">
                          <div className="flex items-center justify-between mb-2">
                            <Label>Build Logs</Label>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setViewingLogsForBuild(null)}
                            >
                              Close
                            </Button>
                          </div>
                          <div className="max-h-[300px] overflow-auto rounded-md border bg-muted/30 p-4">
                            {isLogsLoading ? (
                              <div className="flex items-center justify-center py-4">
                                <Loader2 className="h-4 w-4 animate-spin" />
                              </div>
                            ) : buildLogs?.items && buildLogs.items.length > 0 ? (
                              <pre className="font-mono text-xs leading-relaxed whitespace-pre-wrap">
                                {buildLogs.items
                                  .sort((a, b) => a.sequence - b.sequence)
                                  .map((log) => `[${log.level.toUpperCase()}] ${log.message}`)
                                  .join("\n")}
                              </pre>
                            ) : (
                              <p className="text-sm text-muted-foreground">No logs available</p>
                            )}
                          </div>
                        </div>
                      )}
                      
                      {/* Builds list */}
                      {isBuildsLoading ? (
                        <div className="flex items-center justify-center py-8">
                          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                        </div>
                      ) : builds?.items && builds.items.length > 0 ? (
                        <Table>
                          <TableHeader>
                            <TableRow>
                              <TableHead>Build</TableHead>
                              <TableHead>Branch</TableHead>
                              <TableHead>Status</TableHead>
                              <TableHead>Started</TableHead>
                              <TableHead>Duration</TableHead>
                              <TableHead className="w-[150px]"></TableHead>
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {builds.items.map((build) => (
                              <TableRow key={build.id}>
                                <TableCell className="font-mono text-sm">
                                  {build.id.slice(0, 8)}
                                </TableCell>
                                <TableCell>
                                  <Badge variant="outline">{build.gitRef}</Badge>
                                </TableCell>
                                <TableCell>
                                  <BuildStatusBadge status={build.status} />
                                </TableCell>
                                <TableCell className="text-muted-foreground">
                                  {build.startedAt ? formatRelativeTime(build.startedAt) : "—"}
                                </TableCell>
                                <TableCell className="text-muted-foreground">
                                  {build.durationMs ? `${(build.durationMs / 1000).toFixed(1)}s` : "—"}
                                </TableCell>
                                <TableCell>
                                  <div className="flex items-center gap-1">
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      onClick={() => setViewingLogsForBuild(build.id)}
                                    >
                                      <FileText className="h-4 w-4" />
                                    </Button>
                                    {isBuildInProgress(build.status) && (
                                      <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => cancelBuild.mutate(build.id)}
                                        disabled={cancelBuild.isPending}
                                      >
                                        <XCircle className="h-4 w-4" />
                                      </Button>
                                    )}
                                    {build.status === BuildStatus.FAILED && (
                                      <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => retryBuild.mutate(build.id)}
                                        disabled={retryBuild.isPending}
                                      >
                                        <RotateCcw className="h-4 w-4" />
                                      </Button>
                                    )}
                                  </div>
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      ) : (
                        <p className="text-sm text-muted-foreground text-center py-8">
                          No builds yet. Click "Build" to start your first build.
                        </p>
                      )}
                    </CardContent>
                  )}
                </Card>
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="secrets" className="space-y-4">
          <div className="flex justify-end">
            <Button onClick={() => setCreateSecretOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              New Secret
            </Button>
          </div>

          {isSecretsLoading ? (
            <LoadingState message="Loading secrets..." />
          ) : secrets?.items.length === 0 ? (
            <EmptyState
              icon={<Key className="h-6 w-6" />}
              title="No secrets yet"
              description="Create secrets to store sensitive configuration"
              action={
                <Button onClick={() => setCreateSecretOpen(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  Create Secret
                </Button>
              }
            />
          ) : (
            <SecretsList
              secrets={secrets?.items || []}
              orgId={orgId}
              projectId={projectId}
            />
          )}
        </TabsContent>

        <TabsContent value="deployments" className="space-y-4">
          {isDeploymentsLoading ? (
            <LoadingState message="Loading deployments..." />
          ) : deployments?.items.length === 0 ? (
            <EmptyState
              icon={<Rocket className="h-6 w-6" />}
              title="No deployments yet"
              description="Deploy an application to get started"
            />
          ) : (
            <DeploymentsList
              deployments={deployments?.items || []}
              orgSlug={orgSlug}
            />
          )}
        </TabsContent>
      </Tabs>

      <CreateApplicationDialog
        open={createAppOpen}
        onOpenChange={setCreateAppOpen}
        orgId={orgId}
        projectId={projectId}
      />

      <CreateSecretDialog
        open={createSecretOpen}
        onOpenChange={setCreateSecretOpen}
        orgId={orgId}
        projectId={projectId}
      />
    </div>
  );
}
