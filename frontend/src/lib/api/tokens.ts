import { apiClient } from "./client";
import type { PaginatedResponse } from "@/types/api";

// ─── Response types (mirror backend handler views) ────────────────────────────

export interface ServiceAccountView {
  id: string;
  orgId: string;
  name: string;
  description?: string;
  status: string;
  createdAt: string;
}

export interface APITokenView {
  id: string;
  name: string;
  /** Short prefix like "jwt_a1b2c3d4" used for display — the actual token is NEVER returned here */
  prefix: string;
  scopes: string[];
  expiresAt?: string;
  lastUsedAt?: string;
  revokedAt?: string;
  createdAt: string;
}

/** Returned only on CREATE — token is the plaintext JWT shown exactly once */
export interface APITokenResult {
  id: string;
  name: string;
  prefix: string;
  /** Plaintext JWT. Shown once; never stored/returned again. */
  token: string;
  scopes: string[];
  expiresAt?: string;
  createdAt: string;
}

// ─── Request types ────────────────────────────────────────────────────────────

export interface CreateServiceAccountRequest {
  name: string;
  description?: string;
}

export interface CreateAPITokenRequest {
  name: string;
  scopes?: string[];
  /** 0 = no expiry */
  expiresInDays?: number;
}

// ─── API client ───────────────────────────────────────────────────────────────

export const tokensApi = {
  // Service Accounts
  async createServiceAccount(
    orgId: string,
    data: CreateServiceAccountRequest
  ): Promise<ServiceAccountView> {
    return apiClient.post<ServiceAccountView>(
      `/v1/organizations/${orgId}/service-accounts`,
      data
    );
  },

  async listServiceAccounts(
    orgId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<PaginatedResponse<ServiceAccountView>> {
    return apiClient.get<PaginatedResponse<ServiceAccountView>>(
      `/v1/organizations/${orgId}/service-accounts`,
      params
    );
  },

  async deleteServiceAccount(orgId: string, serviceAccountId: string): Promise<void> {
    return apiClient.delete(
      `/v1/organizations/${orgId}/service-accounts/${serviceAccountId}`
    );
  },

  // API Tokens
  async createAPIToken(
    orgId: string,
    serviceAccountId: string,
    data: CreateAPITokenRequest
  ): Promise<APITokenResult> {
    return apiClient.post<APITokenResult>(
      `/v1/organizations/${orgId}/service-accounts/${serviceAccountId}/tokens`,
      data
    );
  },

  async listAPITokens(
    orgId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<PaginatedResponse<APITokenView>> {
    return apiClient.get<PaginatedResponse<APITokenView>>(
      `/v1/organizations/${orgId}/api-tokens`,
      params
    );
  },

  async revokeAPIToken(orgId: string, tokenId: string): Promise<void> {
    return apiClient.delete(
      `/v1/organizations/${orgId}/api-tokens/${tokenId}`
    );
  },

  // ── Convenience: create a service account + token in one call ───────────────
  // INTENTIONAL SIMPLIFICATION (v1): every call unconditionally creates a NEW
  // service account and issues one token for it. There is no lookup or reuse of
  // an existing SA with the same name. Consequences:
  //   • Two keys named "CI Pipeline" → two distinct SAs in the DB.
  //   • Revoking a token orphans its SA (no token remains attached to it).
  //     The orphaned SA is invisible to the UI (ListAPITokens filters by org,
  //     not by SA), but it accumulates in the service_accounts table.
  // This matches the user-facing "one key = one named thing" model. If we later
  // want SA reuse or multi-token-per-SA, replace this with a dedicated backend
  // endpoint that creates both atomically and handles deduplication server-side.
  async createAPIKey(
    orgId: string,
    opts: {
      name: string;
      description?: string;
      scopes?: string[];
      expiresInDays?: number;
    }
  ): Promise<APITokenResult> {
    const sa = await this.createServiceAccount(orgId, {
      name: opts.name,
      description: opts.description,
    });
    return this.createAPIToken(orgId, sa.id, {
      name: opts.name,
      scopes: opts.scopes,
      expiresInDays: opts.expiresInDays,
    });
  },
};
