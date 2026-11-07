import { apiClient } from "./client";
import type {
  Environment,
  CreateEnvironmentRequest,
  UpdateEnvironmentRequest,
  PromotionRequest,
  RequestPromotionRequest,
  RejectPromotionRequest,
} from "@/types/api";

// Exact paths derived from backend/services/deployment/internal/routes.go:
//
//   POST /v1/organizations/:orgId/applications/:appId/environments  → CreateEnvironment
//   GET  /v1/organizations/:orgId/applications/:appId/environments  → ListEnvironments
//   GET  /v1/organizations/:orgId/environments/:environmentId       → GetEnvironment
//   PATCH /v1/organizations/:orgId/environments/:environmentId      → UpdateEnvironment
//   DELETE /v1/organizations/:orgId/environments/:environmentId     → DeleteEnvironment
//
//   GET  /v1/organizations/:orgId/applications/:appId/promotions    → ListPromotions
//   POST /v1/organizations/:orgId/promotions                        → RequestPromotion
//   GET  /v1/organizations/:orgId/promotions/:promotionId           → GetPromotion
//   POST /v1/organizations/:orgId/promotions/:promotionId/approve   → ApprovePromotion
//   POST /v1/organizations/:orgId/promotions/:promotionId/reject    → RejectPromotion

export const environmentsApi = {
  // -------------------------------------------------------------------------
  // Environments
  // -------------------------------------------------------------------------

  async list(orgId: string, appId: string): Promise<{ items: Environment[] }> {
    return apiClient.get<{ items: Environment[] }>(
      `/v1/organizations/${orgId}/applications/${appId}/environments`
    );
  },

  async get(orgId: string, environmentId: string): Promise<Environment> {
    return apiClient.get<Environment>(
      `/v1/organizations/${orgId}/environments/${environmentId}`
    );
  },

  async create(
    orgId: string,
    appId: string,
    data: CreateEnvironmentRequest
  ): Promise<Environment> {
    return apiClient.post<Environment>(
      `/v1/organizations/${orgId}/applications/${appId}/environments`,
      data
    );
  },

  async update(
    orgId: string,
    environmentId: string,
    data: UpdateEnvironmentRequest
  ): Promise<Environment> {
    return apiClient.patch<Environment>(
      `/v1/organizations/${orgId}/environments/${environmentId}`,
      data
    );
  },

  /**
   * Delete an environment.
   *
   * If any PromotionRequest is still in 'pending' status and references this
   * environment (from_environment_id OR to_environment_id), the backend's
   * ON DELETE RESTRICT constraint will fire and the server returns HTTP 400
   * with code "VALIDATION" and message "referenced resource does not exist".
   *
   * The UI detects this specific case and shows a human-readable message:
   *   "This environment has pending promotions referencing it. Cancel all
   *    pending promotions for this environment before deleting it."
   */
  async delete(orgId: string, environmentId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/environments/${environmentId}`);
  },

  // -------------------------------------------------------------------------
  // Promotions
  // -------------------------------------------------------------------------

  async listPromotions(
    orgId: string,
    appId: string
  ): Promise<{ items: PromotionRequest[] }> {
    return apiClient.get<{ items: PromotionRequest[] }>(
      `/v1/organizations/${orgId}/applications/${appId}/promotions`
    );
  },

  async requestPromotion(
    orgId: string,
    data: RequestPromotionRequest
  ): Promise<PromotionRequest> {
    return apiClient.post<PromotionRequest>(
      `/v1/organizations/${orgId}/promotions`,
      data
    );
  },

  async getPromotion(
    orgId: string,
    promotionId: string
  ): Promise<PromotionRequest> {
    return apiClient.get<PromotionRequest>(
      `/v1/organizations/${orgId}/promotions/${promotionId}`
    );
  },

  async approvePromotion(orgId: string, promotionId: string): Promise<{
    promotion: PromotionRequest;
  }> {
    return apiClient.post<{ promotion: PromotionRequest }>(
      `/v1/organizations/${orgId}/promotions/${promotionId}/approve`
    );
  },

  async rejectPromotion(
    orgId: string,
    promotionId: string,
    data: RejectPromotionRequest
  ): Promise<PromotionRequest> {
    return apiClient.post<PromotionRequest>(
      `/v1/organizations/${orgId}/promotions/${promotionId}/reject`,
      data
    );
  },
};
