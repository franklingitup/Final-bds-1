import { apiClient } from "./client";

// Connection types
export const ConnectionType = {
  OAUTH: "oauth",
  PAT: "pat",
  APP: "app",
} as const;

// Connection status
export const ConnectionStatus = {
  ACTIVE: "active",
  REVOKED: "revoked",
  EXPIRED: "expired",
  INVALID: "invalid",
} as const;

// Webhook status
export const WebhookStatus = {
  ACTIVE: "active",
  INACTIVE: "inactive",
  FAILED: "failed",
} as const;

// API Response types
export interface ConnectionView {
  id: string;
  organizationId: string;
  connectionType: string;
  name: string;
  githubUsername?: string;
  githubAvatar?: string;
  scopes?: string[];
  status: string;
  lastUsedAt?: string;
  lastValidatedAt?: string;
  createdAt: string;
}

export interface GitHubRepoItem {
  id: number;
  name: string;
  fullName: string;
  owner: string;
  description?: string;
  htmlUrl: string;
  cloneUrl: string;
  defaultBranch: string;
  isPrivate: boolean;
  language?: string;
  starsCount: number;
}

export interface RepositoryView {
  id: string;
  organizationId: string;
  connectionId: string;
  githubRepoId: number;
  owner: string;
  name: string;
  fullName: string;
  description?: string;
  htmlUrl: string;
  cloneUrl: string;
  sshUrl?: string;
  defaultBranch: string;
  isPrivate: boolean;
  isFork: boolean;
  isArchived: boolean;
  starsCount: number;
  forksCount: number;
  language?: string;
  topics?: string[];
  lastSyncedAt?: string;
  createdAt: string;
}

export interface WebhookView {
  id: string;
  repositoryId: string;
  githubHookId: number;
  events: string[];
  webhookUrl: string;
  status: string;
  lastDeliveryAt?: string;
  deliveryCount: number;
  createdAt: string;
}

export interface BranchView {
  name: string;
  sha: string;
  protected: boolean;
}

export interface CommitView {
  sha: string;
  message: string;
  author: string;
  authorUrl?: string;
  date: string;
  url: string;
}

export interface OAuthURLResponse {
  url: string;
  state: string;
}

// Request types
export interface CreatePATConnectionRequest {
  name: string;
  accessToken: string;
}

export interface ConnectRepositoryRequest {
  connectionId: string;
  owner: string;
  repo: string;
}

export interface RegisterWebhookRequest {
  events?: string[];
}

// API Client
export const githubApi = {
  // OAuth
  async getOAuthURL(orgId: string, redirectUrl?: string): Promise<OAuthURLResponse> {
    const params = new URLSearchParams();
    if (redirectUrl) params.set("redirectUrl", redirectUrl);
    const qs = params.toString();
    return apiClient.get<OAuthURLResponse>(
      `/v1/organizations/${orgId}/github/oauth/authorize${qs ? `?${qs}` : ""}`
    );
  },

  // Connections
  async createPATConnection(orgId: string, data: CreatePATConnectionRequest): Promise<ConnectionView> {
    return apiClient.post<ConnectionView>(`/v1/organizations/${orgId}/github/connections`, {
      name: data.name,
      connectionType: "pat",
      accessToken: data.accessToken,
    });
  },

  async listConnections(
    orgId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<{ items: ConnectionView[]; nextCursor?: string }> {
    const query = new URLSearchParams();
    if (params?.cursor) query.set("cursor", params.cursor);
    if (params?.limit) query.set("limit", params.limit.toString());
    const qs = query.toString();
    return apiClient.get<{ items: ConnectionView[]; nextCursor?: string }>(
      `/v1/organizations/${orgId}/github/connections${qs ? `?${qs}` : ""}`
    );
  },

  async getConnection(orgId: string, connectionId: string): Promise<ConnectionView> {
    return apiClient.get<ConnectionView>(`/v1/organizations/${orgId}/github/connections/${connectionId}`);
  },

  async deleteConnection(orgId: string, connectionId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/github/connections/${connectionId}`);
  },

  async validateConnection(orgId: string, connectionId: string): Promise<ConnectionView> {
    return apiClient.post<ConnectionView>(
      `/v1/organizations/${orgId}/github/connections/${connectionId}/validate`,
      {}
    );
  },

  // User's GitHub repositories (from GitHub API, not connected yet)
  async listUserGitHubRepositories(
    orgId: string,
    connectionId: string,
    params?: { page?: number; perPage?: number }
  ): Promise<{ items: GitHubRepoItem[] }> {
    const query = new URLSearchParams();
    if (params?.page) query.set("page", params.page.toString());
    if (params?.perPage) query.set("perPage", params.perPage.toString());
    const qs = query.toString();
    return apiClient.get<{ items: GitHubRepoItem[] }>(
      `/v1/organizations/${orgId}/github/connections/${connectionId}/repositories${qs ? `?${qs}` : ""}`
    );
  },

  // Connected repositories
  async connectRepository(orgId: string, data: ConnectRepositoryRequest): Promise<RepositoryView> {
    return apiClient.post<RepositoryView>(`/v1/organizations/${orgId}/github/repositories`, data);
  },

  async listRepositories(
    orgId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<{ items: RepositoryView[]; nextCursor?: string }> {
    const query = new URLSearchParams();
    if (params?.cursor) query.set("cursor", params.cursor);
    if (params?.limit) query.set("limit", params.limit.toString());
    const qs = query.toString();
    return apiClient.get<{ items: RepositoryView[]; nextCursor?: string }>(
      `/v1/organizations/${orgId}/github/repositories${qs ? `?${qs}` : ""}`
    );
  },

  async getRepository(orgId: string, repositoryId: string): Promise<RepositoryView> {
    return apiClient.get<RepositoryView>(`/v1/organizations/${orgId}/github/repositories/${repositoryId}`);
  },

  async deleteRepository(orgId: string, repositoryId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/github/repositories/${repositoryId}`);
  },

  async syncRepository(orgId: string, repositoryId: string): Promise<RepositoryView> {
    return apiClient.post<RepositoryView>(
      `/v1/organizations/${orgId}/github/repositories/${repositoryId}/sync`,
      {}
    );
  },

  // Branches & Commits
  async listBranches(
    orgId: string,
    repositoryId: string,
    params?: { page?: number; perPage?: number }
  ): Promise<{ items: BranchView[] }> {
    const query = new URLSearchParams();
    if (params?.page) query.set("page", params.page.toString());
    if (params?.perPage) query.set("perPage", params.perPage.toString());
    const qs = query.toString();
    return apiClient.get<{ items: BranchView[] }>(
      `/v1/organizations/${orgId}/github/repositories/${repositoryId}/branches${qs ? `?${qs}` : ""}`
    );
  },

  async listCommits(
    orgId: string,
    repositoryId: string,
    params?: { branch?: string; page?: number; perPage?: number }
  ): Promise<{ items: CommitView[] }> {
    const query = new URLSearchParams();
    if (params?.branch) query.set("branch", params.branch);
    if (params?.page) query.set("page", params.page.toString());
    if (params?.perPage) query.set("perPage", params.perPage.toString());
    const qs = query.toString();
    return apiClient.get<{ items: CommitView[] }>(
      `/v1/organizations/${orgId}/github/repositories/${repositoryId}/commits${qs ? `?${qs}` : ""}`
    );
  },

  async getDefaultBranch(orgId: string, repositoryId: string): Promise<{ defaultBranch: string }> {
    return apiClient.get<{ defaultBranch: string }>(
      `/v1/organizations/${orgId}/github/repositories/${repositoryId}/default-branch`
    );
  },

  // Webhooks
  async registerWebhook(
    orgId: string,
    repositoryId: string,
    data?: RegisterWebhookRequest
  ): Promise<WebhookView> {
    return apiClient.post<WebhookView>(
      `/v1/organizations/${orgId}/github/repositories/${repositoryId}/webhook`,
      data || {}
    );
  },

  async getWebhook(orgId: string, webhookId: string): Promise<WebhookView> {
    return apiClient.get<WebhookView>(`/v1/organizations/${orgId}/github/webhooks/${webhookId}`);
  },

  async deleteWebhook(orgId: string, webhookId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/github/webhooks/${webhookId}`);
  },
};
