"use client";

import * as React from "react";
import { useParams, useRouter } from "next/navigation";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Trash2, Settings, Shield, Loader2, Copy, Check, Bell, Plus, TestTube, Mail, MessageSquare, Webhook, ToggleLeft, ToggleRight, Cloud, CheckCircle2, XCircle } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { FormInput, FormTextarea } from "@/components/shared/form-field";
import { LoadingState } from "@/components/shared/loading-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  useUpdateOrganization,
  useDeleteOrganization,
} from "@/hooks/use-organizations";
import { useOrganization } from "@/providers/organization-provider";
import { useAuth } from "@/providers/auth-provider";
import { ConfirmDialog as MFAConfirmDialog } from "@/components/shared/confirm-dialog";
import { authApi, ApiClientError, notificationsApi, provisioningApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/utils";
import Link from "next/link";
import type { CredentialView, AWSCredentials, AzureCredentials, GCPCredentials } from "@/lib/api/provisioning";
import { Provider } from "@/lib/api/provisioning";
import type { ChannelView, PreferenceView, NotificationView, CreateChannelRequest, UpdatePreferenceRequest, SlackConfig, WebhookConfig, EmailConfig } from "@/lib/api/notifications";
import { ChannelType, EventType, NotificationStatus } from "@/lib/api/notifications";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "@/hooks/use-toast";

const updateOrgSchema = z.object({
  name: z.string().min(2, "Name must be at least 2 characters"),
  description: z.string().optional(),
});

type UpdateOrgForm = z.infer<typeof updateOrgSchema>;

export default function SettingsPage() {
  const router = useRouter();
  const params = useParams();
  const orgSlug = params.orgSlug as string;
  const { organization, isOwner } = useOrganization();
  const { user } = useAuth();
  const orgId = organization?.id || "";

  const updateOrg = useUpdateOrganization(orgId);
  const deleteOrg = useDeleteOrganization();

  // MFA state
  const [mfaSetupData, setMfaSetupData] = React.useState<{ secret: string; otpauthUrl: string } | null>(null);
  const [mfaCode, setMfaCode] = React.useState("");
  const [mfaLoading, setMfaLoading] = React.useState(false);
  const [mfaError, setMfaError] = React.useState<string | null>(null);
  const [secretCopied, setSecretCopied] = React.useState(false);

  const handleSetupMFA = async () => {
    setMfaLoading(true);
    setMfaError(null);
    try {
      const result = await authApi.setupMFA();
      setMfaSetupData(result);
    } catch (err) {
      if (ApiClientError.isApiError(err)) {
        setMfaError(err.message);
      } else {
        setMfaError("Failed to setup MFA");
      }
    } finally {
      setMfaLoading(false);
    }
  };

  const handleEnableMFA = async () => {
    if (!mfaCode || mfaCode.length !== 6) {
      setMfaError("Please enter a valid 6-digit code");
      return;
    }
    setMfaLoading(true);
    setMfaError(null);
    try {
      await authApi.enableMFA(mfaCode);
      setMfaSetupData(null);
      setMfaCode("");
      // Force refresh user data
      window.location.reload();
    } catch (err) {
      if (ApiClientError.isApiError(err)) {
        setMfaError(err.message);
      } else {
        setMfaError("Failed to enable MFA");
      }
    } finally {
      setMfaLoading(false);
    }
  };

  const handleDisableMFA = async () => {
    if (!mfaCode || mfaCode.length !== 6) {
      setMfaError("Please enter your current 6-digit code to disable MFA");
      return;
    }
    setMfaLoading(true);
    setMfaError(null);
    try {
      await authApi.disableMFA(mfaCode);
      setMfaCode("");
      // Force refresh user data
      window.location.reload();
    } catch (err) {
      if (ApiClientError.isApiError(err)) {
        setMfaError(err.message);
      } else {
        setMfaError("Failed to disable MFA");
      }
    } finally {
      setMfaLoading(false);
    }
  };

  const copySecret = () => {
    if (mfaSetupData?.secret) {
      navigator.clipboard.writeText(mfaSetupData.secret);
      setSecretCopied(true);
      setTimeout(() => setSecretCopied(false), 2000);
    }
  };

  const methods = useForm<UpdateOrgForm>({
    resolver: zodResolver(updateOrgSchema),
    values: {
      name: organization?.name || "",
      description: organization?.description || "",
    },
  });

  const { handleSubmit, formState: { isSubmitting, isDirty } } = methods;

  const onSubmit = async (data: UpdateOrgForm) => {
    await updateOrg.mutateAsync(data);
  };

  const handleDeleteOrg = async () => {
    await deleteOrg.mutateAsync(orgId);
    router.push("/");
  };

  if (!organization) {
    return <LoadingState />;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Settings"
        description="Manage your organization settings"
      />

      <Tabs defaultValue="general" className="space-y-6">
        <TabsList>
          <TabsTrigger value="general" className="gap-2">
            <Settings className="h-4 w-4" />
            General
          </TabsTrigger>
          <TabsTrigger value="security" className="gap-2">
            <Shield className="h-4 w-4" />
            Security
          </TabsTrigger>
          <TabsTrigger value="notifications" className="gap-2">
            <Bell className="h-4 w-4" />
            Notifications
          </TabsTrigger>
          <TabsTrigger value="cloud-credentials" className="gap-2">
            <Cloud className="h-4 w-4" />
            Cloud Credentials
          </TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Organization Details</CardTitle>
              <CardDescription>
                Update your organization information
              </CardDescription>
            </CardHeader>
            <FormProvider {...methods}>
              <form onSubmit={handleSubmit(onSubmit)}>
                <CardContent className="space-y-4">
                  <FormInput<UpdateOrgForm>
                    name="name"
                    label="Organization Name"
                    required
                  />
                  <FormTextarea<UpdateOrgForm>
                    name="description"
                    label="Description"
                  />
                </CardContent>
                <CardFooter>
                  <Button type="submit" disabled={!isDirty} loading={isSubmitting}>
                    Save Changes
                  </Button>
                </CardFooter>
              </form>
            </FormProvider>
          </Card>

          {isOwner && (
            <Card className="border-destructive">
              <CardHeader>
                <CardTitle className="text-destructive">Danger Zone</CardTitle>
                <CardDescription>
                  Irreversible and destructive actions
                </CardDescription>
              </CardHeader>
              <CardContent>
                <ConfirmDialog
                  trigger={
                    <Button variant="destructive">
                      <Trash2 className="mr-2 h-4 w-4" />
                      Delete Organization
                    </Button>
                  }
                  title="Delete Organization"
                  description="This will permanently delete the organization and all its data. This action cannot be undone."
                  confirmLabel="Delete"
                  variant="destructive"
                  onConfirm={handleDeleteOrg}
                />
              </CardContent>
            </Card>
          )}
        </TabsContent>

        <TabsContent value="security" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Two-Factor Authentication</CardTitle>
              <CardDescription>
                Add an extra layer of security to your account by enabling two-factor authentication (2FA)
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {mfaError && (
                <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
                  {mfaError}
                </div>
              )}

              {user?.mfaEnabled ? (
                // MFA is enabled - show disable option
                <div className="space-y-4">
                  <div className="flex items-center gap-2 text-green-600">
                    <Shield className="h-5 w-5" />
                    <span className="font-medium">Two-factor authentication is enabled</span>
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="disable-mfa-code">Enter your 6-digit code to disable 2FA</Label>
                    <div className="flex gap-2">
                      <Input
                        id="disable-mfa-code"
                        type="text"
                        inputMode="numeric"
                        pattern="[0-9]*"
                        maxLength={6}
                        placeholder="000000"
                        value={mfaCode}
                        onChange={(e) => setMfaCode(e.target.value.replace(/\D/g, ""))}
                        className="w-32 font-mono text-center text-lg tracking-widest"
                      />
                      <MFAConfirmDialog
                        trigger={
                          <Button variant="destructive" disabled={mfaLoading || mfaCode.length !== 6}>
                            {mfaLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : "Disable 2FA"}
                          </Button>
                        }
                        title="Disable Two-Factor Authentication"
                        description="Are you sure you want to disable two-factor authentication? This will make your account less secure."
                        confirmLabel="Disable 2FA"
                        variant="destructive"
                        onConfirm={handleDisableMFA}
                      />
                    </div>
                  </div>
                </div>
              ) : mfaSetupData ? (
                // MFA setup in progress - show QR code and verification
                <div className="space-y-6">
                  <div className="space-y-4">
                    <p className="text-sm text-muted-foreground">
                      Scan this QR code with your authenticator app (like Google Authenticator, Authy, or 1Password):
                    </p>
                    <div className="flex justify-center p-4 bg-white rounded-lg w-fit mx-auto">
                      <QRCodeSVG value={mfaSetupData.otpauthUrl} size={200} />
                    </div>
                  </div>

                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      Or enter this secret key manually:
                    </p>
                    <div className="flex items-center gap-2">
                      <code className="flex-1 px-3 py-2 bg-muted rounded-md font-mono text-sm break-all">
                        {mfaSetupData.secret}
                      </code>
                      <Button variant="outline" size="sm" onClick={copySecret}>
                        {secretCopied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                      </Button>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="mfa-code">Enter the 6-digit code from your authenticator app</Label>
                    <div className="flex gap-2">
                      <Input
                        id="mfa-code"
                        type="text"
                        inputMode="numeric"
                        pattern="[0-9]*"
                        maxLength={6}
                        placeholder="000000"
                        value={mfaCode}
                        onChange={(e) => setMfaCode(e.target.value.replace(/\D/g, ""))}
                        className="w-32 font-mono text-center text-lg tracking-widest"
                      />
                      <Button onClick={handleEnableMFA} disabled={mfaLoading || mfaCode.length !== 6}>
                        {mfaLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : "Verify & Enable"}
                      </Button>
                    </div>
                  </div>

                  <Button variant="outline" onClick={() => { setMfaSetupData(null); setMfaCode(""); setMfaError(null); }}>
                    Cancel
                  </Button>
                </div>
              ) : (
                // MFA not enabled - show enable option
                <div className="space-y-4">
                  <p className="text-sm text-muted-foreground">
                    Two-factor authentication adds an additional layer of security to your account by requiring
                    a verification code from your authenticator app in addition to your password.
                  </p>
                  <Button onClick={handleSetupMFA} disabled={mfaLoading}>
                    {mfaLoading ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Shield className="mr-2 h-4 w-4" />}
                    Enable Two-Factor Authentication
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <NotificationsTabContent orgId={orgId} />
        <CloudCredentialsTabContent orgId={orgId} />
      </Tabs>

    </div>
  );
}

// ---------------------------------------------------------------------------
// Notifications Tab Content
// ---------------------------------------------------------------------------

function NotificationsTabContent({ orgId }: { orgId: string }) {
  const queryClient = useQueryClient();
  const [showAddChannel, setShowAddChannel] = React.useState(false);
  const [newChannel, setNewChannel] = React.useState<{
    name: string;
    channelType: string;
    webhookUrl: string;
    slackChannel: string;
  }>({
    name: "",
    channelType: ChannelType.SLACK,
    webhookUrl: "",
    slackChannel: "",
  });
  const [testingChannel, setTestingChannel] = React.useState<string | null>(null);

  // Fetch channels
  const { data: channelsData, isLoading: channelsLoading } = useQuery({
    queryKey: ["notification-channels", orgId],
    queryFn: () => notificationsApi.listChannels(orgId),
    enabled: !!orgId,
  });

  // Fetch preferences
  const { data: preferencesData, isLoading: prefsLoading } = useQuery({
    queryKey: ["notification-preferences", orgId],
    queryFn: () => notificationsApi.getPreferences(orgId),
    enabled: !!orgId,
  });

  // Fetch notification history
  const { data: historyData, isLoading: historyLoading } = useQuery({
    queryKey: ["notification-history", orgId],
    queryFn: () => notificationsApi.listNotifications(orgId, { limit: 20 }),
    enabled: !!orgId,
  });

  // Create channel mutation
  const createChannel = useMutation({
    mutationFn: (data: CreateChannelRequest) => notificationsApi.createChannel(orgId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notification-channels", orgId] });
      setShowAddChannel(false);
      setNewChannel({ name: "", channelType: ChannelType.SLACK, webhookUrl: "", slackChannel: "" });
      toast({ title: "Channel created" });
    },
    onError: (err: Error) => toast({ title: "Failed to create channel", description: err.message, variant: "destructive" }),
  });

  // Delete channel mutation
  const deleteChannel = useMutation({
    mutationFn: (channelId: string) => notificationsApi.deleteChannel(orgId, channelId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notification-channels", orgId] });
      toast({ title: "Channel deleted" });
    },
    onError: (err: Error) => toast({ title: "Failed to delete channel", description: err.message, variant: "destructive" }),
  });

  // Test channel mutation
  const testChannel = useMutation({
    mutationFn: (channelId: string) => notificationsApi.testChannel(orgId, channelId),
    onSuccess: () => toast({ title: "Test notification sent!" }),
    onError: (err: Error) => toast({ title: "Test failed", description: err.message, variant: "destructive" }),
    onSettled: () => setTestingChannel(null),
  });

  // Update preference mutation
  const updatePreference = useMutation({
    mutationFn: (data: UpdatePreferenceRequest) => notificationsApi.updatePreference(orgId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["notification-preferences", orgId] });
    },
    onError: (err: Error) => toast({ title: "Failed to update preference", description: err.message, variant: "destructive" }),
  });

  const handleCreateChannel = () => {
    if (!newChannel.name) {
      toast({ title: "Channel name is required", variant: "destructive" });
      return;
    }

    let config: SlackConfig | WebhookConfig;
    if (newChannel.channelType === ChannelType.SLACK) {
      if (!newChannel.webhookUrl) {
        toast({ title: "Slack webhook URL is required", variant: "destructive" });
        return;
      }
      config = {
        webhookUrl: newChannel.webhookUrl,
        channel: newChannel.slackChannel || undefined,
      };
    } else if (newChannel.channelType === ChannelType.WEBHOOK) {
      if (!newChannel.webhookUrl) {
        toast({ title: "Webhook URL is required", variant: "destructive" });
        return;
      }
      config = {
        url: newChannel.webhookUrl,
        method: "POST",
      };
    } else {
      toast({ title: "Unsupported channel type", variant: "destructive" });
      return;
    }

    createChannel.mutate({
      name: newChannel.name,
      channelType: newChannel.channelType,
      config,
    });
  };

  const handleTogglePreference = (pref: PreferenceView, field: keyof Pick<PreferenceView, "emailEnabled" | "slackEnabled" | "webhookEnabled" | "inAppEnabled">) => {
    updatePreference.mutate({
      eventType: pref.eventType,
      [field]: !pref[field],
    });
  };

  const getChannelIcon = (type: string) => {
    switch (type) {
      case ChannelType.EMAIL:
        return <Mail className="h-4 w-4" />;
      case ChannelType.SLACK:
        return <MessageSquare className="h-4 w-4" />;
      case ChannelType.WEBHOOK:
        return <Webhook className="h-4 w-4" />;
      default:
        return <Bell className="h-4 w-4" />;
    }
  };

  const formatEventType = (eventType: string) => {
    return eventType
      .replace(/\./g, " ")
      .replace(/_/g, " ")
      .split(" ")
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(" ");
  };

  return (
    <TabsContent value="notifications" className="space-y-4">
      {/* Channels Card */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Notification Channels</CardTitle>
              <CardDescription>
                Configure where notifications are sent (Slack, webhooks, etc.)
              </CardDescription>
            </div>
            <Button onClick={() => setShowAddChannel(!showAddChannel)}>
              <Plus className="mr-2 h-4 w-4" />
              Add Channel
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Add Channel Form */}
          {showAddChannel && (
            <div className="border rounded-lg p-4 space-y-4 bg-muted/50">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="channel-name">Channel Name</Label>
                  <Input
                    id="channel-name"
                    placeholder="e.g., Production Alerts"
                    value={newChannel.name}
                    onChange={(e) => setNewChannel({ ...newChannel, name: e.target.value })}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="channel-type">Channel Type</Label>
                  <Select
                    value={newChannel.channelType}
                    onValueChange={(v) => setNewChannel({ ...newChannel, channelType: v })}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={ChannelType.SLACK}>Slack</SelectItem>
                      <SelectItem value={ChannelType.WEBHOOK}>Webhook</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="webhook-url">
                  {newChannel.channelType === ChannelType.SLACK ? "Slack Webhook URL" : "Webhook URL"}
                </Label>
                <Input
                  id="webhook-url"
                  placeholder={newChannel.channelType === ChannelType.SLACK
                    ? "https://hooks.slack.com/services/..."
                    : "https://your-server.com/webhook"
                  }
                  value={newChannel.webhookUrl}
                  onChange={(e) => setNewChannel({ ...newChannel, webhookUrl: e.target.value })}
                />
              </div>
              {newChannel.channelType === ChannelType.SLACK && (
                <div className="space-y-2">
                  <Label htmlFor="slack-channel">Slack Channel (optional)</Label>
                  <Input
                    id="slack-channel"
                    placeholder="#alerts"
                    value={newChannel.slackChannel}
                    onChange={(e) => setNewChannel({ ...newChannel, slackChannel: e.target.value })}
                  />
                </div>
              )}
              <div className="flex gap-2">
                <Button onClick={handleCreateChannel} disabled={createChannel.isPending}>
                  {createChannel.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                  Create Channel
                </Button>
                <Button variant="outline" onClick={() => setShowAddChannel(false)}>
                  Cancel
                </Button>
              </div>
            </div>
          )}

          {/* Channels List */}
          {channelsLoading ? (
            <LoadingState message="Loading channels..." />
          ) : channelsData?.channels && channelsData.channels.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Channel</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="w-[200px]">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {channelsData.channels.map((channel) => (
                  <TableRow key={channel.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        {getChannelIcon(channel.channelType)}
                        <span className="font-medium">{channel.name}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="capitalize">
                        {channel.channelType}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant={channel.enabled ? "default" : "secondary"}>
                        {channel.enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            setTestingChannel(channel.id);
                            testChannel.mutate(channel.id);
                          }}
                          disabled={testingChannel === channel.id}
                        >
                          {testingChannel === channel.id ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <TestTube className="h-4 w-4" />
                          )}
                          <span className="ml-1">Test</span>
                        </Button>
                        <ConfirmDialog
                          trigger={
                            <Button variant="ghost" size="sm">
                              <Trash2 className="h-4 w-4 text-destructive" />
                            </Button>
                          }
                          title="Delete Channel"
                          description={`Delete the "${channel.name}" notification channel?`}
                          confirmLabel="Delete"
                          variant="destructive"
                          onConfirm={() => deleteChannel.mutate(channel.id)}
                        />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground text-center py-4">
              No notification channels configured yet.
            </p>
          )}
        </CardContent>
      </Card>

      {/* Preferences Card */}
      <Card>
        <CardHeader>
          <CardTitle>Event Preferences</CardTitle>
          <CardDescription>
            Choose which events trigger notifications and through which channels
          </CardDescription>
        </CardHeader>
        <CardContent>
          {prefsLoading ? (
            <LoadingState message="Loading preferences..." />
          ) : preferencesData?.preferences && preferencesData.preferences.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Event</TableHead>
                  <TableHead className="text-center">Email</TableHead>
                  <TableHead className="text-center">Slack</TableHead>
                  <TableHead className="text-center">Webhook</TableHead>
                  <TableHead className="text-center">In-App</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {preferencesData.preferences.map((pref) => (
                  <TableRow key={pref.id}>
                    <TableCell className="font-medium">
                      {formatEventType(pref.eventType)}
                    </TableCell>
                    <TableCell className="text-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleTogglePreference(pref, "emailEnabled")}
                      >
                        {pref.emailEnabled ? (
                          <ToggleRight className="h-5 w-5 text-green-600" />
                        ) : (
                          <ToggleLeft className="h-5 w-5 text-muted-foreground" />
                        )}
                      </Button>
                    </TableCell>
                    <TableCell className="text-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleTogglePreference(pref, "slackEnabled")}
                      >
                        {pref.slackEnabled ? (
                          <ToggleRight className="h-5 w-5 text-green-600" />
                        ) : (
                          <ToggleLeft className="h-5 w-5 text-muted-foreground" />
                        )}
                      </Button>
                    </TableCell>
                    <TableCell className="text-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleTogglePreference(pref, "webhookEnabled")}
                      >
                        {pref.webhookEnabled ? (
                          <ToggleRight className="h-5 w-5 text-green-600" />
                        ) : (
                          <ToggleLeft className="h-5 w-5 text-muted-foreground" />
                        )}
                      </Button>
                    </TableCell>
                    <TableCell className="text-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleTogglePreference(pref, "inAppEnabled")}
                      >
                        {pref.inAppEnabled ? (
                          <ToggleRight className="h-5 w-5 text-green-600" />
                        ) : (
                          <ToggleLeft className="h-5 w-5 text-muted-foreground" />
                        )}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground text-center py-4">
              No event preferences configured. Preferences will appear after channels are set up.
            </p>
          )}
        </CardContent>
      </Card>

      {/* History Card */}
      <Card>
        <CardHeader>
          <CardTitle>Notification History</CardTitle>
          <CardDescription>
            Recent notifications sent from this organization
          </CardDescription>
        </CardHeader>
        <CardContent>
          {historyLoading ? (
            <LoadingState message="Loading history..." />
          ) : historyData?.notifications && historyData.notifications.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Event</TableHead>
                  <TableHead>Title</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Sent</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {historyData.notifications.map((notif) => (
                  <TableRow key={notif.id}>
                    <TableCell>
                      <Badge variant="outline">
                        {formatEventType(notif.eventType)}
                      </Badge>
                    </TableCell>
                    <TableCell className="max-w-[300px] truncate">
                      {notif.title}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          notif.status === NotificationStatus.SENT
                            ? "default"
                            : notif.status === NotificationStatus.FAILED
                            ? "destructive"
                            : "secondary"
                        }
                      >
                        {notif.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatRelativeTime(notif.createdAt)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground text-center py-4">
              No notifications sent yet.
            </p>
          )}
        </CardContent>
      </Card>
    </TabsContent>
  );
}

// ---------------------------------------------------------------------------
// Cloud Credentials Tab Content
// ---------------------------------------------------------------------------

function CloudCredentialsTabContent({ orgId }: { orgId: string }) {
  const queryClient = useQueryClient();
  const [showAddCredential, setShowAddCredential] = React.useState(false);
  const [selectedProvider, setSelectedProvider] = React.useState<string>(Provider.AWS);
  const [credentialName, setCredentialName] = React.useState("");
  const [credentialDescription, setCredentialDescription] = React.useState("");
  const [validatingId, setValidatingId] = React.useState<string | null>(null);
  const [deletingId, setDeletingId] = React.useState<string | null>(null);
  
  // AWS credential fields
  const [awsAccessKeyId, setAwsAccessKeyId] = React.useState("");
  const [awsSecretAccessKey, setAwsSecretAccessKey] = React.useState("");
  const [awsSessionToken, setAwsSessionToken] = React.useState("");
  const [awsRoleArn, setAwsRoleArn] = React.useState("");
  const [awsExternalId, setAwsExternalId] = React.useState("");
  
  // Azure credential fields
  const [azureSubscriptionId, setAzureSubscriptionId] = React.useState("");
  const [azureTenantId, setAzureTenantId] = React.useState("");
  const [azureClientId, setAzureClientId] = React.useState("");
  const [azureClientSecret, setAzureClientSecret] = React.useState("");
  
  // GCP credential fields
  const [gcpProjectId, setGcpProjectId] = React.useState("");
  const [gcpServiceAccountJson, setGcpServiceAccountJson] = React.useState("");

  // Fetch credentials
  const { data: credentialsData, isLoading: credentialsLoading, refetch } = useQuery({
    queryKey: ["credentials", orgId],
    queryFn: () => provisioningApi.listCredentials(orgId),
    enabled: !!orgId,
  });

  // Create credential mutation
  const createCredential = useMutation({
    mutationFn: (data: { name: string; provider: string; credentials: AWSCredentials | AzureCredentials | GCPCredentials; description?: string }) =>
      provisioningApi.createCredential(orgId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["credentials", orgId] });
      resetForm();
      setShowAddCredential(false);
      toast({ title: "Credential created successfully" });
    },
    onError: (err: ApiClientError) => {
      toast({ title: "Failed to create credential", description: err.message, variant: "destructive" });
    },
  });

  // Validate credential mutation
  const validateCredential = useMutation({
    mutationFn: (credentialId: string) => provisioningApi.validateCredential(orgId, credentialId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["credentials", orgId] });
      if (data.validated) {
        toast({ title: "Credential validated successfully" });
      } else {
        toast({
          title: "Credential validation failed",
          description: data.validationError || "Unknown error",
          variant: "destructive",
        });
      }
      setValidatingId(null);
    },
    onError: (err: ApiClientError) => {
      toast({ title: "Validation failed", description: err.message, variant: "destructive" });
      setValidatingId(null);
    },
  });

  // Delete credential mutation
  const deleteCredential = useMutation({
    mutationFn: (credentialId: string) => provisioningApi.deleteCredential(orgId, credentialId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["credentials", orgId] });
      setDeletingId(null);
      toast({ title: "Credential deleted" });
    },
    onError: (err: ApiClientError) => {
      toast({ title: "Failed to delete credential", description: err.message, variant: "destructive" });
      setDeletingId(null);
    },
  });

  const resetForm = () => {
    setCredentialName("");
    setCredentialDescription("");
    setSelectedProvider(Provider.AWS);
    setAwsAccessKeyId("");
    setAwsSecretAccessKey("");
    setAwsSessionToken("");
    setAwsRoleArn("");
    setAwsExternalId("");
    setAzureSubscriptionId("");
    setAzureTenantId("");
    setAzureClientId("");
    setAzureClientSecret("");
    setGcpProjectId("");
    setGcpServiceAccountJson("");
  };

  const handleCreateCredential = async () => {
    if (!credentialName.trim()) {
      toast({ title: "Name is required", variant: "destructive" });
      return;
    }

    let credentials: AWSCredentials | AzureCredentials | GCPCredentials;

    switch (selectedProvider) {
      case Provider.AWS:
        if (!awsAccessKeyId || !awsSecretAccessKey) {
          toast({ title: "AWS Access Key ID and Secret Access Key are required", variant: "destructive" });
          return;
        }
        credentials = {
          accessKeyId: awsAccessKeyId,
          secretAccessKey: awsSecretAccessKey,
          sessionToken: awsSessionToken || undefined,
          roleArn: awsRoleArn || undefined,
          externalId: awsExternalId || undefined,
        };
        break;
      case Provider.AZURE:
        if (!azureSubscriptionId || !azureTenantId || !azureClientId || !azureClientSecret) {
          toast({ title: "All Azure fields are required", variant: "destructive" });
          return;
        }
        credentials = {
          subscriptionId: azureSubscriptionId,
          tenantId: azureTenantId,
          clientId: azureClientId,
          clientSecret: azureClientSecret,
        };
        break;
      case Provider.GCP:
        if (!gcpProjectId || !gcpServiceAccountJson) {
          toast({ title: "GCP Project ID and Service Account JSON are required", variant: "destructive" });
          return;
        }
        credentials = {
          projectId: gcpProjectId,
          serviceAccountJson: gcpServiceAccountJson,
        };
        break;
      default:
        return;
    }

    createCredential.mutate({
      name: credentialName,
      provider: selectedProvider,
      credentials,
      description: credentialDescription || undefined,
    });
  };

  const handleValidate = (credentialId: string) => {
    setValidatingId(credentialId);
    validateCredential.mutate(credentialId);
  };

  const handleDelete = (credentialId: string) => {
    setDeletingId(credentialId);
    deleteCredential.mutate(credentialId);
  };

  const getProviderLabel = (provider: string) => {
    switch (provider) {
      case Provider.AWS:
        return "Amazon Web Services (AWS)";
      case Provider.GCP:
        return "Google Cloud Platform (GCP)";
      case Provider.AZURE:
        return "Microsoft Azure";
      default:
        return provider;
    }
  };

  return (
    <TabsContent value="cloud-credentials" className="space-y-4">
      {/* Credentials List Card */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Cloud Credentials</CardTitle>
              <CardDescription>
                Manage cloud provider credentials for cluster provisioning
              </CardDescription>
            </div>
            <Button onClick={() => setShowAddCredential(true)}>
              <Plus className="mr-2 h-4 w-4" />
              Add Credential
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {credentialsLoading ? (
            <LoadingState message="Loading credentials..." />
          ) : credentialsData?.credentials && credentialsData.credentials.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-[150px]"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {credentialsData.credentials.map((cred) => (
                  <TableRow key={cred.id}>
                    <TableCell>
                      <div>
                        <p className="font-medium">{cred.name}</p>
                        {cred.description && (
                          <p className="text-sm text-muted-foreground">{cred.description}</p>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{getProviderLabel(cred.provider)}</Badge>
                    </TableCell>
                    <TableCell>
                      {cred.validated ? (
                        <div className="flex items-center gap-2">
                          <CheckCircle2 className="h-4 w-4 text-green-600" />
                          <span className="text-sm text-green-600">Validated</span>
                        </div>
                      ) : cred.validationError ? (
                        <div className="flex items-center gap-2">
                          <XCircle className="h-4 w-4 text-destructive" />
                          <span className="text-sm text-destructive" title={cred.validationError}>
                            Invalid
                          </span>
                        </div>
                      ) : (
                        <span className="text-sm text-muted-foreground">Not validated</span>
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatRelativeTime(cred.createdAt)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleValidate(cred.id)}
                          disabled={validatingId === cred.id}
                        >
                          {validatingId === cred.id ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <TestTube className="h-4 w-4" />
                          )}
                        </Button>
                        <ConfirmDialog
                          trigger={
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={deletingId === cred.id}
                            >
                              {deletingId === cred.id ? (
                                <Loader2 className="h-4 w-4 animate-spin" />
                              ) : (
                                <Trash2 className="h-4 w-4" />
                              )}
                            </Button>
                          }
                          title="Delete Credential"
                          description={`Are you sure you want to delete "${cred.name}"? This cannot be undone.`}
                          confirmLabel="Delete"
                          variant="destructive"
                          onConfirm={() => handleDelete(cred.id)}
                        />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="flex flex-col items-center justify-center py-8">
              <Cloud className="h-12 w-12 text-muted-foreground mb-4" />
              <p className="text-muted-foreground text-center">
                No cloud credentials configured yet.
                <br />
                Add credentials to enable cloud cluster provisioning.
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Add Credential Form */}
      {showAddCredential && (
        <Card>
          <CardHeader>
            <CardTitle>Add Cloud Credential</CardTitle>
            <CardDescription>
              Add credentials for your cloud provider to enable cluster provisioning
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="cred-name">Name</Label>
                <Input
                  id="cred-name"
                  placeholder="e.g., Production AWS Account"
                  value={credentialName}
                  onChange={(e) => setCredentialName(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="cred-provider">Cloud Provider</Label>
                <Select value={selectedProvider} onValueChange={setSelectedProvider}>
                  <SelectTrigger id="cred-provider">
                    <SelectValue placeholder="Select provider" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={Provider.AWS}>Amazon Web Services (AWS)</SelectItem>
                    <SelectItem value={Provider.GCP}>Google Cloud Platform (GCP)</SelectItem>
                    <SelectItem value={Provider.AZURE}>Microsoft Azure</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="cred-description">Description (optional)</Label>
              <Input
                id="cred-description"
                placeholder="e.g., Used for production EKS clusters"
                value={credentialDescription}
                onChange={(e) => setCredentialDescription(e.target.value)}
              />
            </div>

            {/* AWS Credentials */}
            {selectedProvider === Provider.AWS && (
              <div className="space-y-4 p-4 border rounded-lg bg-muted/50">
                <h4 className="font-medium">AWS Credentials</h4>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="aws-access-key">Access Key ID *</Label>
                    <Input
                      id="aws-access-key"
                      placeholder="AKIAIOSFODNN7EXAMPLE"
                      value={awsAccessKeyId}
                      onChange={(e) => setAwsAccessKeyId(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="aws-secret-key">Secret Access Key *</Label>
                    <Input
                      id="aws-secret-key"
                      type="password"
                      placeholder="••••••••••••••••"
                      value={awsSecretAccessKey}
                      onChange={(e) => setAwsSecretAccessKey(e.target.value)}
                    />
                  </div>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="aws-session-token">Session Token (optional)</Label>
                  <Input
                    id="aws-session-token"
                    type="password"
                    placeholder="For temporary credentials"
                    value={awsSessionToken}
                    onChange={(e) => setAwsSessionToken(e.target.value)}
                  />
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="aws-role-arn">Role ARN (optional)</Label>
                    <Input
                      id="aws-role-arn"
                      placeholder="arn:aws:iam::123456789012:role/ExampleRole"
                      value={awsRoleArn}
                      onChange={(e) => setAwsRoleArn(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="aws-external-id">External ID (optional)</Label>
                    <Input
                      id="aws-external-id"
                      placeholder="For cross-account access"
                      value={awsExternalId}
                      onChange={(e) => setAwsExternalId(e.target.value)}
                    />
                  </div>
                </div>
              </div>
            )}

            {/* Azure Credentials */}
            {selectedProvider === Provider.AZURE && (
              <div className="space-y-4 p-4 border rounded-lg bg-muted/50">
                <h4 className="font-medium">Azure Credentials</h4>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="azure-subscription-id">Subscription ID *</Label>
                    <Input
                      id="azure-subscription-id"
                      placeholder="00000000-0000-0000-0000-000000000000"
                      value={azureSubscriptionId}
                      onChange={(e) => setAzureSubscriptionId(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="azure-tenant-id">Tenant ID *</Label>
                    <Input
                      id="azure-tenant-id"
                      placeholder="00000000-0000-0000-0000-000000000000"
                      value={azureTenantId}
                      onChange={(e) => setAzureTenantId(e.target.value)}
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="azure-client-id">Client ID *</Label>
                    <Input
                      id="azure-client-id"
                      placeholder="00000000-0000-0000-0000-000000000000"
                      value={azureClientId}
                      onChange={(e) => setAzureClientId(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="azure-client-secret">Client Secret *</Label>
                    <Input
                      id="azure-client-secret"
                      type="password"
                      placeholder="••••••••••••••••"
                      value={azureClientSecret}
                      onChange={(e) => setAzureClientSecret(e.target.value)}
                    />
                  </div>
                </div>
              </div>
            )}

            {/* GCP Credentials */}
            {selectedProvider === Provider.GCP && (
              <div className="space-y-4 p-4 border rounded-lg bg-muted/50">
                <h4 className="font-medium">GCP Credentials</h4>
                <div className="space-y-2">
                  <Label htmlFor="gcp-project-id">Project ID *</Label>
                  <Input
                    id="gcp-project-id"
                    placeholder="my-gcp-project-123"
                    value={gcpProjectId}
                    onChange={(e) => setGcpProjectId(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="gcp-service-account">Service Account JSON *</Label>
                  <textarea
                    id="gcp-service-account"
                    className="flex min-h-[150px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 font-mono"
                    placeholder='{"type": "service_account", ...}'
                    value={gcpServiceAccountJson}
                    onChange={(e) => setGcpServiceAccountJson(e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">
                    Paste the complete JSON key file contents from your GCP service account.
                  </p>
                </div>
              </div>
            )}
          </CardContent>
          <CardFooter className="flex justify-end gap-2">
            <Button
              variant="outline"
              onClick={() => {
                resetForm();
                setShowAddCredential(false);
              }}
            >
              Cancel
            </Button>
            <Button onClick={handleCreateCredential} disabled={createCredential.isPending}>
              {createCredential.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Add Credential
            </Button>
          </CardFooter>
        </Card>
      )}
    </TabsContent>
  );
}
