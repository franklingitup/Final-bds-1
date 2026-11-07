"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import {
  Users,
  UserPlus,
  Key,
  Plus,
  Trash2,
  Copy,
  Check,
  Loader2,
  Eye,
  EyeOff,
  ShieldCheck,
} from "lucide-react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { InviteMemberDialog } from "@/components/organizations/invite-member-dialog";
import {
  useOrganizationMembers,
  useUpdateMemberRole,
  useRemoveMember,
  useOrganizationInvitations,
  useRevokeInvitation,
} from "@/hooks/use-organizations";
import { useOrganization } from "@/providers/organization-provider";
import { formatRelativeTime } from "@/lib/utils";
import { tokensApi, type APITokenResult } from "@/lib/api/tokens";
import { toast } from "@/hooks/use-toast";

// ─── Helpers ─────────────────────────────────────────────────────────────────

const EXPIRY_OPTIONS = [
  { label: "No expiry", value: 0 },
  { label: "30 days", value: 30 },
  { label: "90 days", value: 90 },
  { label: "1 year (365 days)", value: 365 },
];

// ─── Token-revealed dialog (shown exactly once after creation) ────────────────

function NewTokenDialog({
  result,
  onClose,
}: {
  result: APITokenResult;
  onClose: () => void;
}) {
  const [copied, setCopied] = React.useState(false);
  const [revealed, setRevealed] = React.useState(false);
  const [confirmed, setConfirmed] = React.useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(result.token);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Dialog open onOpenChange={() => confirmed && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="h-5 w-5 text-green-600" />
            API Key Created
          </DialogTitle>
          <DialogDescription>
            Copy your API key now — it will <strong>never be shown again</strong> after you close
            this dialog.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground uppercase tracking-wide">Name</Label>
            <p className="font-medium">{result.name}</p>
          </div>

          {result.scopes.length > 0 && (
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground uppercase tracking-wide">Scopes</Label>
              <div className="flex flex-wrap gap-1">
                {result.scopes.map((s) => (
                  <Badge key={s} variant="secondary">
                    {s}
                  </Badge>
                ))}
              </div>
            </div>
          )}

          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground uppercase tracking-wide">Token</Label>
            <div className="flex items-center gap-2">
              <div className="flex-1 rounded-md border bg-muted px-3 py-2 font-mono text-sm overflow-hidden">
                {revealed ? (
                  <span className="break-all">{result.token}</span>
                ) : (
                  <span className="text-muted-foreground">{"•".repeat(40)}</span>
                )}
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setRevealed((v) => !v)}
                title={revealed ? "Hide token" : "Reveal token"}
              >
                {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </Button>
              <Button variant="outline" size="icon" onClick={handleCopy} title="Copy token">
                {copied ? <Check className="h-4 w-4 text-green-600" /> : <Copy className="h-4 w-4" />}
              </Button>
            </div>
          </div>

          <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
            <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" />
            <span>
              Store this token in a secure location (e.g. a secrets manager). You won&apos;t be able
              to retrieve it again.
            </span>
          </div>

          <label className="flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
              className="rounded border-gray-300"
            />
            <span className="text-sm">I have saved my token in a safe place</span>
          </label>
        </div>

        <DialogFooter>
          <Button onClick={onClose} disabled={!confirmed}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── Create API Key dialog ────────────────────────────────────────────────────

function CreateAPIKeyDialog({
  orgId,
  onCreated,
  onClose,
}: {
  orgId: string;
  onCreated: (result: APITokenResult) => void;
  onClose: () => void;
}) {
  const [name, setName] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [expiresInDays, setExpiresInDays] = React.useState(0);

  const create = useMutation({
    mutationFn: () =>
      tokensApi.createAPIKey(orgId, {
        name: name.trim(),
        description: description.trim() || undefined,
        expiresInDays: expiresInDays > 0 ? expiresInDays : undefined,
      }),
    onSuccess: (result) => {
      onCreated(result);
    },
    onError: (err: Error) => {
      toast({ title: "Failed to create API key", description: err.message, variant: "destructive" });
    },
  });

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create API Key</DialogTitle>
          <DialogDescription>
            API keys allow programmatic access to this organization. The token will be shown once.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="space-y-2">
            <Label htmlFor="key-name">Name *</Label>
            <Input
              id="key-name"
              placeholder="e.g. CI/CD Pipeline"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="key-desc">Description (optional)</Label>
            <Input
              id="key-desc"
              placeholder="What is this key used for?"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="key-expiry">Expiry</Label>
            <Select
              value={String(expiresInDays)}
              onValueChange={(v) => setExpiresInDays(Number(v))}
            >
              <SelectTrigger id="key-expiry">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {EXPIRY_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={String(o.value)}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={() => create.mutate()}
            disabled={!name.trim() || create.isPending}
          >
            {create.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Create Key
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─── API Keys tab content ─────────────────────────────────────────────────────

function APIKeysTab({ orgId }: { orgId: string }) {
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = React.useState(false);
  const [newToken, setNewToken] = React.useState<APITokenResult | null>(null);

  const { data: tokens, isLoading } = useQuery({
    queryKey: ["api-tokens", orgId],
    queryFn: () => tokensApi.listAPITokens(orgId),
    enabled: !!orgId,
  });

  const revoke = useMutation({
    mutationFn: (id: string) => tokensApi.revokeAPIToken(orgId, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-tokens", orgId] });
      toast({ title: "API key revoked" });
    },
    onError: (err: Error) => {
      toast({ title: "Failed to revoke key", description: err.message, variant: "destructive" });
    },
  });

  const handleCreated = (result: APITokenResult) => {
    queryClient.invalidateQueries({ queryKey: ["api-tokens", orgId] });
    setShowCreate(false);
    setNewToken(result);
  };

  return (
    <>
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>API Keys</CardTitle>
              <CardDescription>
                Programmatic access tokens for CI/CD pipelines, scripts, and integrations.
                Each key is shown exactly once on creation.
              </CardDescription>
            </div>
            <Button onClick={() => setShowCreate(true)}>
              <Plus className="mr-2 h-4 w-4" />
              Create API Key
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <LoadingState message="Loading API keys..." />
          ) : tokens?.items && tokens.items.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Prefix</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead>Last used</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="w-[80px]"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tokens.items.map((token) => (
                  <TableRow key={token.id} className={token.revokedAt ? "opacity-50" : ""}>
                    <TableCell className="font-medium">{token.name}</TableCell>
                    <TableCell>
                      <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
                        {token.prefix}
                      </code>
                    </TableCell>
                    <TableCell>
                      {token.scopes.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {token.scopes.map((s) => (
                            <Badge key={s} variant="secondary" className="text-xs">
                              {s}
                            </Badge>
                          ))}
                        </div>
                      ) : (
                        <span className="text-sm text-muted-foreground">Full access</span>
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {token.expiresAt ? formatRelativeTime(token.expiresAt) : "Never"}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {token.lastUsedAt ? formatRelativeTime(token.lastUsedAt) : "Never"}
                    </TableCell>
                    <TableCell>
                      {token.revokedAt ? (
                        <Badge variant="destructive" className="text-xs">Revoked</Badge>
                      ) : token.expiresAt && new Date(token.expiresAt) < new Date() ? (
                        <Badge variant="secondary" className="text-xs">Expired</Badge>
                      ) : (
                        <Badge variant="default" className="text-xs">Active</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {!token.revokedAt && (
                        <ConfirmDialog
                          trigger={
                            <Button variant="ghost" size="sm" className="text-destructive">
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          }
                          title="Revoke API Key"
                          description={`Revoke "${token.name}"? Any services using this key will immediately lose access.`}
                          confirmLabel="Revoke"
                          variant="destructive"
                          onConfirm={() => revoke.mutate(token.id)}
                        />
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="flex flex-col items-center justify-center py-12 text-center">
              <Key className="h-10 w-10 text-muted-foreground mb-3" />
              <p className="font-medium">No API keys yet</p>
              <p className="text-sm text-muted-foreground mt-1">
                Create a key to enable programmatic access from CI/CD pipelines or scripts.
              </p>
              <Button className="mt-4" onClick={() => setShowCreate(true)}>
                <Plus className="mr-2 h-4 w-4" />
                Create API Key
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      {showCreate && (
        <CreateAPIKeyDialog
          orgId={orgId}
          onCreated={handleCreated}
          onClose={() => setShowCreate(false)}
        />
      )}

      {newToken && (
        <NewTokenDialog result={newToken} onClose={() => setNewToken(null)} />
      )}
    </>
  );
}

// ─── Members tab content (same data as Settings → Members) ───────────────────

function MembersTab({ orgId }: { orgId: string }) {
  const { canManageMembers } = useOrganization();
  const [inviteOpen, setInviteOpen] = React.useState(false);

  const { data: members, isLoading: isMembersLoading } = useOrganizationMembers(orgId);
  const { data: invitations } = useOrganizationInvitations(orgId);
  const updateRole = useUpdateMemberRole(orgId);
  const removeMember = useRemoveMember(orgId);
  const revokeInvitation = useRevokeInvitation(orgId);

  return (
    <>
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Members</CardTitle>
              <CardDescription>Manage organization members and their roles</CardDescription>
            </div>
            {canManageMembers && (
              <Button onClick={() => setInviteOpen(true)}>
                <UserPlus className="mr-2 h-4 w-4" />
                Invite Member
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {isMembersLoading ? (
            <LoadingState message="Loading members..." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>User</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Joined</TableHead>
                  <TableHead className="w-[100px]"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {members?.items.map((member) =>
                  member.user ? (
                    <TableRow key={member.id}>
                      <TableCell>
                        <div className="flex items-center gap-3">
                          <Avatar className="h-8 w-8">
                            <AvatarImage src={member.user.avatarUrl} />
                            <AvatarFallback>
                              {member.user.name?.[0]?.toUpperCase() || "U"}
                            </AvatarFallback>
                          </Avatar>
                          <div>
                            <p className="font-medium">{member.user.name}</p>
                            <p className="text-sm text-muted-foreground">{member.user.email}</p>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        {canManageMembers && member.role !== "owner" ? (
                          <Select
                            value={member.role}
                            onValueChange={(role) =>
                              updateRole.mutate({ memberId: member.id, role })
                            }
                          >
                            <SelectTrigger className="w-[120px]">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="admin">Admin</SelectItem>
                              <SelectItem value="member">Member</SelectItem>
                              <SelectItem value="viewer">Viewer</SelectItem>
                            </SelectContent>
                          </Select>
                        ) : (
                          <Badge variant="outline" className="capitalize">
                            {member.role}
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatRelativeTime(member.createdAt)}
                      </TableCell>
                      <TableCell>
                        {canManageMembers && member.role !== "owner" && (
                          <ConfirmDialog
                            trigger={
                              <Button variant="ghost" size="sm">
                                Remove
                              </Button>
                            }
                            title="Remove Member"
                            description={`Remove ${member.user.name} from this organization?`}
                            confirmLabel="Remove"
                            variant="destructive"
                            onConfirm={() => removeMember.mutate(member.id)}
                          />
                        )}
                      </TableCell>
                    </TableRow>
                  ) : (
                    <TableRow key={member.id}>
                      <TableCell colSpan={4} className="text-sm text-muted-foreground">
                        Member record incomplete (user data unavailable)
                      </TableCell>
                    </TableRow>
                  )
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {invitations?.items && invitations.items.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Pending Invitations</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Email</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="w-[100px]"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {invitations.items.map((inv) => (
                  <TableRow key={inv.id}>
                    <TableCell>{inv.email}</TableCell>
                    <TableCell>
                      <Badge variant="outline" className="capitalize">
                        {inv.role}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatRelativeTime(inv.expiresAt)}
                    </TableCell>
                    <TableCell>
                      {canManageMembers && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => revokeInvitation.mutate(inv.id)}
                        >
                          Revoke
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <InviteMemberDialog
        open={inviteOpen}
        onOpenChange={setInviteOpen}
        orgId={orgId}
      />
    </>
  );
}

// ─── Page ─────────────────────────────────────────────────────────────────────

export default function AccessPage() {
  const params = useParams();
  const { organization } = useOrganization();
  const orgId = organization?.id || "";

  if (!organization) return <LoadingState />;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Access"
        description="Manage who can access this organization and how they authenticate"
      />

      <Tabs defaultValue="members" className="space-y-6">
        <TabsList>
          <TabsTrigger value="members" className="gap-2">
            <Users className="h-4 w-4" />
            Members
          </TabsTrigger>
          <TabsTrigger value="api-keys" className="gap-2">
            <Key className="h-4 w-4" />
            API Keys
          </TabsTrigger>
        </TabsList>

        <TabsContent value="members" className="space-y-4">
          <MembersTab orgId={orgId} />
        </TabsContent>

        <TabsContent value="api-keys" className="space-y-4">
          <APIKeysTab orgId={orgId} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
