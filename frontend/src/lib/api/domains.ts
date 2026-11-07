import { apiClient } from "./client";
import type { PaginatedResponse } from "@/types/api";

// Domain verification status constants
export const VerificationStatus = {
  PENDING: "pending",
  VERIFYING: "verifying",
  VERIFIED: "verified",
  FAILED: "failed",
} as const;

// Domain status constants
export const DomainStatus = {
  PENDING: "pending",
  ACTIVE: "active",
  SUSPENDED: "suspended",
  DELETED: "deleted",
} as const;

// Certificate status constants
export const CertificateStatus = {
  PENDING: "pending",
  ISSUING: "issuing",
  ACTIVE: "active",
  EXPIRED: "expired",
  REVOKED: "revoked",
  FAILED: "failed",
} as const;

// DNS records needed for verification
export interface DNSRecords {
  txtName: string;
  txtValue: string;
  cnameHost?: string;
}

// Certificate view
export interface CertificateView {
  id: string;
  commonName: string;
  issuer: string;
  status: string;
  issuedAt?: string;
  expiresAt?: string;
  daysToExpiry?: number;
}

// Domain view (API response)
export interface DomainView {
  id: string;
  organizationId: string;
  deploymentId: string;
  domain: string;
  subdomain?: string;
  fullDomain: string;
  verificationStatus: string;
  verificationMethod: string;
  verifiedAt?: string;
  dnsRecords: DNSRecords;
  status: string;
  isPrimary: boolean;
  certificate?: CertificateView;
  createdAt: string;
}

// Request types
export interface CreateDomainRequest {
  deploymentId: string;
  domain: string;
  subdomain?: string;
}

export interface UpdateDomainRequest {
  isPrimary?: boolean;
}

export interface VerifyDomainRequest {
  force?: boolean;
}

export interface IssueCertificateRequest {
  forceRenewal?: boolean;
}

export const domainsApi = {
  /**
   * Create a new custom domain for a deployment.
   */
  async create(orgId: string, request: CreateDomainRequest): Promise<DomainView> {
    return apiClient.post<DomainView>(`/v1/organizations/${orgId}/domains`, request);
  },

  /**
   * List all domains in the organization.
   */
  async list(orgId: string): Promise<PaginatedResponse<DomainView>> {
    return apiClient.get<PaginatedResponse<DomainView>>(`/v1/organizations/${orgId}/domains`);
  },

  /**
   * List domains attached to a specific deployment.
   */
  async listByDeployment(orgId: string, deploymentId: string): Promise<PaginatedResponse<DomainView>> {
    return apiClient.get<PaginatedResponse<DomainView>>(
      `/v1/organizations/${orgId}/deployments/${deploymentId}/domains`
    );
  },

  /**
   * Get a specific domain by ID.
   */
  async get(orgId: string, domainId: string): Promise<DomainView> {
    return apiClient.get<DomainView>(`/v1/organizations/${orgId}/domains/${domainId}`);
  },

  /**
   * Update a domain (e.g., set as primary).
   */
  async update(orgId: string, domainId: string, request: UpdateDomainRequest): Promise<DomainView> {
    return apiClient.patch<DomainView>(`/v1/organizations/${orgId}/domains/${domainId}`, request);
  },

  /**
   * Delete a domain.
   */
  async delete(orgId: string, domainId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/domains/${domainId}`);
  },

  /**
   * Verify domain ownership via DNS.
   */
  async verify(orgId: string, domainId: string, request?: VerifyDomainRequest): Promise<DomainView> {
    return apiClient.post<DomainView>(`/v1/organizations/${orgId}/domains/${domainId}/verify`, request || {});
  },

  /**
   * Issue or renew a TLS certificate for the domain.
   */
  async issueCertificate(orgId: string, domainId: string, request?: IssueCertificateRequest): Promise<CertificateView> {
    return apiClient.post<CertificateView>(`/v1/organizations/${orgId}/domains/${domainId}/certificate`, request || {});
  },

  /**
   * Get the certificate for a domain.
   */
  async getCertificate(orgId: string, domainId: string): Promise<CertificateView> {
    return apiClient.get<CertificateView>(`/v1/organizations/${orgId}/domains/${domainId}/certificate`);
  },
};
