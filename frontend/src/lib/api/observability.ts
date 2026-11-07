import { apiClient } from "./client";

// Logs query request/response types
export interface LogsQueryRequest {
  query: string;
  start?: string;
  end?: string;
  limit?: number;
  direction?: "forward" | "backward";
}

export interface LogStreamData {
  stream: Record<string, string>;
  values: [string, string][]; // [[timestamp, line], ...]
}

export interface LogsQueryResult {
  resultType: "streams";
  result: LogStreamData[];
}

export interface LogsResponse {
  status: string;
  data: LogsQueryResult;
}

// Metrics types
export interface MetricValue {
  value: number;
  unit?: string;
  trend?: "up" | "down" | "stable";
  change?: number;
  threshold?: "ok" | "warning" | "critical";
}

export interface ResourceMetrics {
  resourceType: string;
  resourceId: string;
  resourceName?: string;
  metrics: Record<string, MetricValue>;
  health: string;
  updatedAt: string;
}

export const observabilityApi = {
  /**
   * Query logs from Loki (or internal storage).
   * For a deployment, use query like: {deployment_id="deploymentId"}
   */
  async queryLogs(orgId: string, request: LogsQueryRequest): Promise<LogsResponse> {
    return apiClient.post<LogsResponse>(`/v1/organizations/${orgId}/logs/query`, request);
  },

  /**
   * Get aggregated metrics for a specific resource.
   */
  async getResourceMetrics(
    orgId: string,
    resourceType: string,
    resourceId: string
  ): Promise<ResourceMetrics> {
    return apiClient.get<ResourceMetrics>(
      `/v1/organizations/${orgId}/metrics/${resourceType}/${resourceId}`
    );
  },
};
