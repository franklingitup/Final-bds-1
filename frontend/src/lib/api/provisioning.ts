import { apiClient } from "./client";

// Provider constants
export const Provider = {
  AWS: "aws",
  GCP: "gcp",
  AZURE: "azure",
} as const;

// Status constants
export const RequestStatus = {
  PENDING: "pending",
  GENERATING: "generating",
  READY: "ready",
  PROVISIONING: "provisioning",
  COMPLETED: "completed",
  FAILED: "failed",
  CANCELLED: "cancelled",
} as const;

export const SessionStatus = {
  ACTIVE: "active",
  COMPLETED: "completed",
  FAILED: "failed",
  EXPIRED: "expired",
  CANCELLED: "cancelled",
} as const;

export const StepStatus = {
  PENDING: "pending",
  RUNNING: "running",
  COMPLETED: "completed",
  FAILED: "failed",
  SKIPPED: "skipped",
} as const;

// Types
export interface AWSCredentials {
  accessKeyId: string;
  secretAccessKey: string;
  sessionToken?: string;
  roleArn?: string;
  externalId?: string;
}

export interface AzureCredentials {
  subscriptionId: string;
  tenantId: string;
  clientId: string;
  clientSecret: string;
}

export interface GCPCredentials {
  projectId: string;
  serviceAccountJson: string;
}

export type CloudCredentials = AWSCredentials | AzureCredentials | GCPCredentials;

export interface CredentialView {
  id: string;
  name: string;
  provider: string;
  validated: boolean;
  validatedAt?: string;
  validationError?: string;
  region?: string;
  description?: string;
  createdAt: string;
}

export interface NodePool {
  name: string;
  machineType: string;
  minNodes: number;
  maxNodes: number;
  desiredNodes: number;
  diskSizeGb: number;
  diskType?: string;
  preemptible?: boolean;
  labels?: Record<string, string>;
  taints?: NodeTaint[];
}

export interface NodeTaint {
  key: string;
  value: string;
  effect: string;
}

export interface NetworkingConfig {
  vpcCidr?: string;
  podCidr?: string;
  serviceCidr?: string;
  subnetCidrs?: string[];
  privateCluster?: boolean;
  natGateway?: boolean;
}

export interface ClusterAddon {
  name: string;
  enabled: boolean;
  config?: Record<string, unknown>;
}

export interface TemplateView {
  id: string;
  name: string;
  provider: string;
  k8sVersion: string;
  nodePools: NodePool[];
  networking?: NetworkingConfig;
  addons?: ClusterAddon[];
  description?: string;
  isDefault: boolean;
  createdAt: string;
}

export interface ProvisioningRequestView {
  id: string;
  name: string;
  provider: string;
  region: string;
  k8sVersion: string;
  nodePools: NodePool[];
  networking?: NetworkingConfig;
  addons?: ClusterAddon[];
  status: string;
  clusterId?: string;
  errorMessage?: string;
  terraformConfig?: string;
  terraformVars?: Record<string, unknown>;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
}

export interface StepInfo {
  number: number;
  name: string;
  description?: string;
  status: string;
}

export interface StepView {
  id: string;
  stepNumber: number;
  name: string;
  description?: string;
  status: string;
  output?: string;
  error?: string;
  startedAt?: string;
  completedAt?: string;
  durationMs?: number;
}

export interface InstallSessionView {
  id: string;
  requestId: string;
  sessionToken: string;
  currentStep: string;
  totalSteps: number;
  completedSteps: number;
  steps: StepInfo[];
  status: string;
  bootstrapCommand?: string;
  agentConnected: boolean;
  agentVersion?: string;
  expiresAt: string;
  createdAt: string;
}

export interface EventView {
  id: string;
  eventType: string;
  severity: string;
  message: string;
  details?: Record<string, unknown>;
  actorType?: string;
  createdAt: string;
}

export interface RegionInfo {
  code: string;
  name: string;
  available: boolean;
}

export interface MachineTypeInfo {
  name: string;
  vcpus: number;
  memoryGb: number;
  description?: string;
}

export interface K8sVersionInfo {
  version: string;
  isDefault: boolean;
  deprecated: boolean;
}

// Request types
export interface CreateCredentialRequest {
  name: string;
  provider: string;
  credentials: CloudCredentials;
  region?: string;
  description?: string;
}

export interface CreateProvisioningRequest {
  name: string;
  provider: string;
  region: string;
  credentialId?: string;
  templateId?: string;
  k8sVersion?: string;
  nodePools?: NodePool[];
  networking?: NetworkingConfig;
  addons?: ClusterAddon[];
}

// API Client
export const provisioningApi = {
  // Cloud Credentials
  async createCredential(orgId: string, data: CreateCredentialRequest): Promise<CredentialView> {
    return apiClient.post<CredentialView>(`/v1/organizations/${orgId}/credentials`, data);
  },

  async listCredentials(orgId: string): Promise<{ credentials: CredentialView[] }> {
    return apiClient.get<{ credentials: CredentialView[] }>(`/v1/organizations/${orgId}/credentials`);
  },

  async validateCredential(orgId: string, credentialId: string): Promise<CredentialView> {
    return apiClient.post<CredentialView>(`/v1/organizations/${orgId}/credentials/${credentialId}/validate`, {});
  },

  async deleteCredential(orgId: string, credentialId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/credentials/${credentialId}`);
  },

  // Templates
  async listTemplates(
    orgId: string,
    provider?: string
  ): Promise<{ templates: TemplateView[] }> {
    const params: Record<string, string> = {};
    if (provider) params.provider = provider;
    return apiClient.get<{ templates: TemplateView[] }>(
      `/v1/organizations/${orgId}/templates`,
      Object.keys(params).length > 0 ? params : undefined
    );
  },

  // Provisioning Requests
  async createProvisioningRequest(
    orgId: string,
    data: CreateProvisioningRequest
  ): Promise<ProvisioningRequestView> {
    return apiClient.post<ProvisioningRequestView>(`/v1/organizations/${orgId}/provisioning`, data);
  },

  async listProvisioningRequests(
    orgId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<{ requests: ProvisioningRequestView[]; nextCursor?: string }> {
    const query: Record<string, string | number> = {};
    if (params?.cursor) query.cursor = params.cursor;
    if (params?.limit) query.limit = params.limit;
    return apiClient.get<{ requests: ProvisioningRequestView[]; nextCursor?: string }>(
      `/v1/organizations/${orgId}/provisioning`,
      Object.keys(query).length > 0 ? query : undefined
    );
  },

  async getProvisioningRequest(orgId: string, requestId: string): Promise<ProvisioningRequestView> {
    return apiClient.get<ProvisioningRequestView>(`/v1/organizations/${orgId}/provisioning/${requestId}`);
  },

  async generateTerraform(orgId: string, requestId: string): Promise<ProvisioningRequestView> {
    return apiClient.post<ProvisioningRequestView>(
      `/v1/organizations/${orgId}/provisioning/${requestId}/terraform`,
      {}
    );
  },

  async startProvisioning(orgId: string, requestId: string): Promise<InstallSessionView> {
    return apiClient.post<InstallSessionView>(
      `/v1/organizations/${orgId}/provisioning/${requestId}/start`,
      {}
    );
  },

  async listEvents(
    orgId: string,
    requestId: string,
    params?: { sessionId?: string; cursor?: string; limit?: number }
  ): Promise<{ events: EventView[]; nextCursor?: string }> {
    const query: Record<string, string | number> = {};
    if (params?.sessionId) query.sessionId = params.sessionId;
    if (params?.cursor) query.cursor = params.cursor;
    if (params?.limit) query.limit = params.limit;
    return apiClient.get<{ events: EventView[]; nextCursor?: string }>(
      `/v1/organizations/${orgId}/provisioning/${requestId}/events`,
      Object.keys(query).length > 0 ? query : undefined
    );
  },

  // Install Sessions
  async getInstallSession(
    orgId: string,
    sessionId: string
  ): Promise<{ session: InstallSessionView; steps: StepView[] }> {
    return apiClient.get<{ session: InstallSessionView; steps: StepView[] }>(
      `/v1/organizations/${orgId}/sessions/${sessionId}`
    );
  },

  // Provider Info
  async listRegions(
    orgId: string,
    provider: string,
    credentialId?: string
  ): Promise<{ regions: RegionInfo[] }> {
    const params: Record<string, string> = {};
    if (credentialId) params.credentialId = credentialId;
    return apiClient.get<{ regions: RegionInfo[] }>(
      `/v1/organizations/${orgId}/providers/${provider}/regions`,
      Object.keys(params).length > 0 ? params : undefined
    );
  },

  async listMachineTypes(
    orgId: string,
    provider: string,
    region: string
  ): Promise<{ machineTypes: MachineTypeInfo[] }> {
    return apiClient.get<{ machineTypes: MachineTypeInfo[] }>(
      `/v1/organizations/${orgId}/providers/${provider}/machine-types`,
      { region }
    );
  },

  async listKubernetesVersions(
    orgId: string,
    provider: string
  ): Promise<{ versions: K8sVersionInfo[] }> {
    return apiClient.get<{ versions: K8sVersionInfo[] }>(
      `/v1/organizations/${orgId}/providers/${provider}/kubernetes-versions`
    );
  },
};
