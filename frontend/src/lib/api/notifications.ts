import { apiClient } from "./client";

// Channel types
export const ChannelType = {
  EMAIL: "email",
  SLACK: "slack",
  WEBHOOK: "webhook",
  IN_APP: "in_app",
} as const;

// Status constants
export const NotificationStatus = {
  PENDING: "pending",
  SENDING: "sending",
  SENT: "sent",
  FAILED: "failed",
  DLQ: "dlq",
} as const;

export const DeliveryStatus = {
  PENDING: "pending",
  SENDING: "sending",
  DELIVERED: "delivered",
  FAILED: "failed",
  DLQ: "dlq",
} as const;

// Event types that trigger notifications
export const EventType = {
  DEPLOYMENT_STARTED: "deployment.started",
  DEPLOYMENT_COMPLETED: "deployment.completed",
  DEPLOYMENT_FAILED: "deployment.failed",
  DEPLOYMENT_ROLLED_BACK: "deployment.rolled_back",
  BUILD_STARTED: "build.started",
  BUILD_COMPLETED: "build.completed",
  BUILD_FAILED: "build.failed",
  ALERT_FIRING: "alert.firing",
  ALERT_RESOLVED: "alert.resolved",
  CLUSTER_HEALTHY: "cluster.healthy",
  CLUSTER_UNHEALTHY: "cluster.unhealthy",
  CERT_EXPIRING: "certificate.expiring",
  INVITATION_SENT: "invitation.sent",
} as const;

// Channel config types
export interface EmailConfig {
  smtpHost: string;
  smtpPort: number;
  smtpUser: string;
  smtpPassword?: string;
  fromAddress: string;
  fromName: string;
  useTLS: boolean;
}

export interface SlackConfig {
  webhookUrl: string;
  channel?: string;
  username?: string;
  iconEmoji?: string;
}

export interface WebhookConfig {
  url: string;
  method: string;
  headers?: Record<string, string>;
  secret?: string;
}

// API Response types
export interface ChannelView {
  id: string;
  name: string;
  channelType: string;
  config: EmailConfig | SlackConfig | WebhookConfig | Record<string, unknown>;
  enabled: boolean;
  verified: boolean;
  verifiedAt?: string;
  createdAt: string;
}

export interface PreferenceView {
  id: string;
  eventType: string;
  emailEnabled: boolean;
  slackEnabled: boolean;
  webhookEnabled: boolean;
  inAppEnabled: boolean;
  severityFilter: string[];
}

export interface NotificationView {
  id: string;
  eventType: string;
  title: string;
  body: string;
  severity: string;
  resourceType?: string;
  resourceId?: string;
  resourceName?: string;
  metadata?: Record<string, unknown>;
  status: string;
  createdAt: string;
}

export interface DeliveryView {
  id: string;
  channelType: string;
  recipient: string;
  status: string;
  attemptCount: number;
  sentAt?: string;
  errorMessage?: string;
}

export interface WebhookSubscriptionView {
  id: string;
  name: string;
  url: string;
  eventTypes: string[];
  headers?: Record<string, string>;
  enabled: boolean;
  lastTriggeredAt?: string;
  successCount: number;
  failureCount: number;
  createdAt: string;
}

// Request types
export interface CreateChannelRequest {
  name: string;
  channelType: string;
  config: EmailConfig | SlackConfig | WebhookConfig;
}

export interface UpdateChannelRequest {
  name?: string;
  config?: EmailConfig | SlackConfig | WebhookConfig;
  enabled?: boolean;
}

export interface UpdatePreferenceRequest {
  eventType: string;
  emailEnabled?: boolean;
  slackEnabled?: boolean;
  webhookEnabled?: boolean;
  inAppEnabled?: boolean;
  severityFilter?: string[];
}

export interface CreateWebhookRequest {
  name: string;
  url: string;
  eventTypes: string[];
  headers?: Record<string, string>;
}

// API Client
export const notificationsApi = {
  // Channels
  async createChannel(orgId: string, data: CreateChannelRequest): Promise<ChannelView> {
    return apiClient.post<ChannelView>(`/v1/organizations/${orgId}/channels`, data);
  },

  async listChannels(orgId: string): Promise<{ channels: ChannelView[] }> {
    return apiClient.get<{ channels: ChannelView[] }>(`/v1/organizations/${orgId}/channels`);
  },

  async getChannel(orgId: string, channelId: string): Promise<ChannelView> {
    return apiClient.get<ChannelView>(`/v1/organizations/${orgId}/channels/${channelId}`);
  },

  async updateChannel(orgId: string, channelId: string, data: UpdateChannelRequest): Promise<ChannelView> {
    return apiClient.patch<ChannelView>(`/v1/organizations/${orgId}/channels/${channelId}`, data);
  },

  async deleteChannel(orgId: string, channelId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/channels/${channelId}`);
  },

  async testChannel(orgId: string, channelId: string): Promise<{ message: string }> {
    return apiClient.post<{ message: string }>(`/v1/organizations/${orgId}/channels/${channelId}/test`, {});
  },

  // Preferences
  async getPreferences(orgId: string): Promise<{ preferences: PreferenceView[] }> {
    return apiClient.get<{ preferences: PreferenceView[] }>(`/v1/organizations/${orgId}/preferences`);
  },

  async updatePreference(orgId: string, data: UpdatePreferenceRequest): Promise<PreferenceView> {
    // The backend uses PUT for this endpoint
    return apiClient.request<PreferenceView>(`/v1/organizations/${orgId}/preferences`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  },

  // Notifications
  async listNotifications(
    orgId: string,
    params?: { cursor?: string; limit?: number }
  ): Promise<{ notifications: NotificationView[]; nextCursor?: string }> {
    const query = new URLSearchParams();
    if (params?.cursor) query.set("cursor", params.cursor);
    if (params?.limit) query.set("limit", params.limit.toString());
    const qs = query.toString();
    return apiClient.get<{ notifications: NotificationView[]; nextCursor?: string }>(
      `/v1/organizations/${orgId}/notifications${qs ? `?${qs}` : ""}`
    );
  },

  async getNotification(
    orgId: string,
    notificationId: string
  ): Promise<{ notification: NotificationView; deliveries: DeliveryView[] }> {
    return apiClient.get<{ notification: NotificationView; deliveries: DeliveryView[] }>(
      `/v1/organizations/${orgId}/notifications/${notificationId}`
    );
  },

  // Webhooks (outbound subscriptions, not the same as notification channels)
  async createWebhook(orgId: string, data: CreateWebhookRequest): Promise<WebhookSubscriptionView> {
    return apiClient.post<WebhookSubscriptionView>(`/v1/organizations/${orgId}/webhooks`, data);
  },

  async listWebhooks(orgId: string): Promise<{ webhooks: WebhookSubscriptionView[] }> {
    return apiClient.get<{ webhooks: WebhookSubscriptionView[] }>(`/v1/organizations/${orgId}/webhooks`);
  },

  async deleteWebhook(orgId: string, webhookId: string): Promise<void> {
    return apiClient.delete(`/v1/organizations/${orgId}/webhooks/${webhookId}`);
  },
};
