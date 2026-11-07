import { apiClient } from "./client";
import type { PaginatedResponse } from "@/types/api";

// Build status constants
export const BuildStatus = {
  QUEUED: "queued",
  CLONING: "cloning",
  BUILDING: "building",
  PUSHING: "pushing",
  SUCCEEDED: "succeeded",
  FAILED: "failed",
  CANCELLED: "cancelled",
} as const;

// Auth type constants
export const AuthType = {
  NONE: "none",
  TOKEN: "token",
  SSH_KEY: "ssh_key",
  DEPLOY_KEY: "deploy_key",
} as const;

// Repository view (API response)
export interface RepositoryView {
  id: string;
  organizationId: string;
  projectId: string;
  name: string;
  url: string;
  defaultBranch: string;
  authType: string;
  createdAt: string;
}

// Build view (API response)
export interface BuildView {
  id: string;
  organizationId: string;
  repositoryId?: string;
  gitUrl?: string;
  gitRef: string;
  gitCommit?: string;
  contextPath: string;
  dockerfilePath: string;
  buildArgs?: Record<string, string>;
  targetImage: string;
  targetRegistry: string;
  pushToRegistry: boolean;
  builderType: string;
  status: string;
  queuedAt: string;
  startedAt?: string;
  finishedAt?: string;
  durationMs?: number;
  errorMessage?: string;
  retryCount: number;
  maxRetries: number;
  parentBuildId?: string;
  createdAt: string;
}

// Build log view
export interface BuildLogView {
  id: string;
  buildId: string;
  sequence: number;
  timestamp: string;
  stream: string;
  level: string;
  message: string;
  metadata?: Record<string, unknown>;
}

// Build artifact view
export interface BuildArtifactView {
  id: string;
  buildId: string;
  imageDigest: string;
  imageTag: string;
  imageSize?: number;
  manifestType: string;
  layerCount?: number;
  dockerfileHash?: string;
  buildDurationMs?: number;
  labels?: Record<string, string>;
  createdAt: string;
}

// Request types
export interface CreateRepositoryRequest {
  projectId: string;
  name: string;
  url: string;
  defaultBranch?: string;
  authType?: string;
  authSecretId?: string;
}

export interface UpdateRepositoryRequest {
  name?: string;
  url?: string;
  defaultBranch?: string;
  authType?: string;
  authSecretId?: string;
}

export interface CreateBuildRequest {
  repositoryId?: string;
  gitUrl?: string;
  gitRef?: string;
  contextPath?: string;
  dockerfilePath?: string;
  buildArgs?: Record<string, string>;
  targetImage: string;
  targetRegistry: string;
  pushToRegistry?: boolean;
  builderType?: string;
  cpuLimit?: string;
  memoryLimit?: string;
  timeoutSeconds?: number;
}

export interface RetryBuildRequest {
  resetRetryCount?: boolean;
}

export const buildsApi = {
  // ============================================================================
  // Repository Management
  // ============================================================================

  /**
   * Create a new git repository connection.
   */
  async createRepository(orgId: string, projectId: string, request: Omit<CreateRepositoryRequest, "projectId">): Promise<RepositoryView> {
    return apiClient.post<RepositoryView>(
      `/v1/organizations/${orgId}/projects/${projectId}/repositories`,
      { ...request, projectId }
    );
  },

  /**
   * List repositories in a project.
   */
  async listRepositories(orgId: string, projectId: string): Promise<PaginatedResponse<RepositoryView>> {
    return apiClient.get<PaginatedResponse<RepositoryView>>(
      `/v1/organizations/${orgId}/projects/${projectId}/repositories`
    );
  },

  /**
   * Get a specific repository.
   */
  async getRepository(orgId: string, repoId: string): Promise<RepositoryView> {
    return apiClient.get<RepositoryView>(`/v1/organizations/${orgId}/repositories/${repoId}`);
  },

  /**
   * Update a repository.
   */
  async updateRepository(orgId: string, repoId: string, request: UpdateRepositoryRequest): Promise<RepositoryView> {
    return apiClient.patch<RepositoryView>(`/v1/organizations/${orgId}/repositories/${repoId}`, request);
  },

  /**
   * Delete a repository.
   */
  async deleteRepository(orgId: string, repoId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/repositories/${repoId}`);
  },

  // ============================================================================
  // Build Management
  // ============================================================================

  /**
   * Trigger a new build.
   */
  async createBuild(orgId: string, request: CreateBuildRequest): Promise<BuildView> {
    return apiClient.post<BuildView>(`/v1/organizations/${orgId}/builds`, request);
  },

  /**
   * List builds in the organization.
   */
  async listBuilds(
    orgId: string,
    params?: { repositoryId?: string; limit?: number; cursor?: string }
  ): Promise<PaginatedResponse<BuildView>> {
    return apiClient.get<PaginatedResponse<BuildView>>(`/v1/organizations/${orgId}/builds`, params);
  },

  /**
   * Get a specific build.
   */
  async getBuild(orgId: string, buildId: string): Promise<BuildView> {
    return apiClient.get<BuildView>(`/v1/organizations/${orgId}/builds/${buildId}`);
  },

  /**
   * Cancel a running build.
   */
  async cancelBuild(orgId: string, buildId: string): Promise<BuildView> {
    return apiClient.post<BuildView>(`/v1/organizations/${orgId}/builds/${buildId}/cancel`, {});
  },

  /**
   * Retry a failed build.
   */
  async retryBuild(orgId: string, buildId: string, request?: RetryBuildRequest): Promise<BuildView> {
    return apiClient.post<BuildView>(`/v1/organizations/${orgId}/builds/${buildId}/retry`, request || {});
  },

  /**
   * Get build logs.
   */
  async getBuildLogs(orgId: string, buildId: string): Promise<PaginatedResponse<BuildLogView>> {
    return apiClient.get<PaginatedResponse<BuildLogView>>(`/v1/organizations/${orgId}/builds/${buildId}/logs`);
  },

  /**
   * Get build artifact (output image details).
   */
  async getBuildArtifact(orgId: string, buildId: string): Promise<BuildArtifactView> {
    return apiClient.get<BuildArtifactView>(`/v1/organizations/${orgId}/builds/${buildId}/artifact`);
  },
};
