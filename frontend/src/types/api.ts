// API Response types

// API-CRIT-05: Backend returns nested error envelope
export interface ApiErrorDetail {
  code: string;
  message: string;
  details?: string[];
  requestId?: string;
}

export interface ApiError {
  error: ApiErrorDetail;
}

// API-CRIT-06: Backend returns nextCursor, not cursor
export interface PaginatedResponse<T> {
  items: T[];
  nextCursor?: string;
  hasMore?: boolean;
}

// Auth types
export interface User {
  id: string;
  email: string;
  name: string;
  avatarUrl?: string;
  createdAt: string;
}

export interface AuthTokens {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface SignupRequest {
  email: string;
  password: string;
  name: string;
}

// Organization types
export interface Organization {
  id: string;
  name: string;
  slug: string;
  description?: string;
  logoUrl?: string;
  createdAt: string;
  updatedAt: string;
}

export interface OrganizationMember {
  id: string;
  userId: string;
  organizationId: string;
  role: "owner" | "admin" | "member" | "viewer";
  user: User;
  createdAt: string;
}

export interface CreateOrganizationRequest {
  name: string;
  slug: string;
  description?: string;
}

export interface UpdateOrganizationRequest {
  name?: string;
  description?: string;
}

export interface InviteMemberRequest {
  email: string;
  role: "admin" | "member" | "viewer";
}

export interface Invitation {
  id: string;
  email: string;
  role: string;
  status: "pending" | "accepted" | "expired";
  expiresAt: string;
  createdAt: string;
}

// Project types
export interface Project {
  id: string;
  organizationId: string;
  name: string;
  slug: string;
  description?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectMember {
  id: string;
  userId: string;
  projectId: string;
  role: "admin" | "developer" | "viewer";
  user: User;
  createdAt: string;
}

export interface CreateProjectRequest {
  name: string;
  slug: string;
  description?: string;
}

export interface UpdateProjectRequest {
  name?: string;
  description?: string;
}

// Cluster types
export interface Cluster {
  id: string;
  organizationId: string;
  name: string;
  slug: string;
  description?: string;
  status: "pending" | "connected" | "disconnected" | "deleted";
  cloudProvider?: string;
  region?: string;
  kubernetesVersion?: string;
  nodeCount?: number;
  agentId?: string;
  registeredAt?: string;
  lastHeartbeatAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CreateClusterRequest {
  name: string;
  slug: string;
  description?: string;
  cloudProvider?: string;
  region?: string;
}

export interface RegistrationToken {
  id: string;
  token: string;
  status: "active" | "used" | "expired" | "revoked";
  expiresAt: string;
  createdAt: string;
}

export interface Heartbeat {
  id: string;
  agentId: string;
  kubernetesVersion: string;
  nodeCount: number;
  apiServerHealthy: boolean;
  receivedAt: string;
}

// Secret types
export interface Secret {
  id: string;
  organizationId: string;
  projectId: string;
  name: string;
  description?: string;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface CreateSecretRequest {
  name: string;
  value: string;
  description?: string;
}

export interface UpdateSecretRequest {
  value?: string;
  description?: string;
}

// Application types
export interface Application {
  id: string;
  organizationId: string;
  projectId: string;
  name: string;
  slug: string;
  description?: string;
  runtimeType: "container" | "function" | "job";
  createdAt: string;
  updatedAt: string;
}

export interface CreateApplicationRequest {
  name: string;
  slug: string;
  description?: string;
  runtimeType?: string;
}

export interface UpdateApplicationRequest {
  name?: string;
  description?: string;
}

// Deployment types
export interface EnvVar {
  name: string;
  value: string;
}

export interface Deployment {
  id: string;
  organizationId: string;
  applicationId: string;
  clusterId: string;
  image: string;
  replicas: number;
  cpuRequest?: string;
  cpuLimit?: string;
  memoryRequest?: string;
  memoryLimit?: string;
  port?: number;
  envVars?: EnvVar[];
  status: "pending" | "running" | "succeeded" | "failed" | "rolled_back";
  readyReplicas: number;
  desiredReplicas: number;
  currentRevision?: number;
  createdAt: string;
  updatedAt: string;
}

export interface CreateDeploymentRequest {
  applicationId: string;
  clusterId: string;
  image: string;
  replicas: number;
  cpuRequest?: string;
  cpuLimit?: string;
  memoryRequest?: string;
  memoryLimit?: string;
  port?: number;
  envVars?: EnvVar[];
}

export interface UpdateDeploymentRequest {
  image?: string;
  replicas?: number;
  cpuRequest?: string;
  cpuLimit?: string;
  memoryRequest?: string;
  memoryLimit?: string;
  port?: number;
  envVars?: EnvVar[];
}

export interface Release {
  id: string;
  deploymentId: string;
  revision: number;
  image: string;
  replicas: number;
  status: "pending" | "deploying" | "succeeded" | "failed" | "rolled_back";
  startedAt?: string;
  finishedAt?: string;
  errorMessage?: string;
  createdAt: string;
}

export interface RollbackRequest {
  targetRevision?: number;
}

// Audit types
export interface AuditLog {
  id: string;
  organizationId: string;
  actorType: string;
  actorId: string;
  actorName?: string;
  action: string;
  resourceType: string;
  resourceId: string;
  resourceName?: string;
  metadata?: Record<string, unknown>;
  ipAddress?: string;
  userAgent?: string;
  createdAt: string;
}

export interface AuditLogFilter {
  actorId?: string;
  action?: string;
  resourceType?: string;
  resourceId?: string;
  startDate?: string;
  endDate?: string;
}
