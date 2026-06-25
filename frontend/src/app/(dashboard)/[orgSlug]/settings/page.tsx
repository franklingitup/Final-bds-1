"use client";

import * as React from "react";
import { useParams, useRouter } from "next/navigation";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Trash2, Users, Settings, UserPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
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
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { FormInput, FormTextarea } from "@/components/shared/form-field";
import { LoadingState } from "@/components/shared/loading-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  useOrganizationMembers,
  useUpdateMemberRole,
  useRemoveMember,
  useUpdateOrganization,
  useDeleteOrganization,
  useOrganizationInvitations,
  useRevokeInvitation,
} from "@/hooks/use-organizations";
import { useOrganization } from "@/providers/organization-provider";
import { InviteMemberDialog } from "@/components/organizations/invite-member-dialog";
import { formatRelativeTime } from "@/lib/utils";

const updateOrgSchema = z.object({
  name: z.string().min(2, "Name must be at least 2 characters"),
  description: z.string().optional(),
});

type UpdateOrgForm = z.infer<typeof updateOrgSchema>;

export default function SettingsPage() {
  const router = useRouter();
  const params = useParams();
  const orgSlug = params.orgSlug as string;
  const { organization, isOwner, canManageMembers } = useOrganization();
  const orgId = organization?.id || "";

  const { data: members, isLoading: isMembersLoading } = useOrganizationMembers(orgId);
  const { data: invitations } = useOrganizationInvitations(orgId);
  const updateOrg = useUpdateOrganization(orgId);
  const deleteOrg = useDeleteOrganization();
  const updateRole = useUpdateMemberRole(orgId);
  const removeMember = useRemoveMember(orgId);
  const revokeInvitation = useRevokeInvitation(orgId);

  const [inviteDialogOpen, setInviteDialogOpen] = React.useState(false);

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
          <TabsTrigger value="members" className="gap-2">
            <Users className="h-4 w-4" />
            Members
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

        <TabsContent value="members" className="space-y-4">
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle>Members</CardTitle>
                  <CardDescription>
                    Manage organization members and their roles
                  </CardDescription>
                </div>
                {canManageMembers && (
                  <Button onClick={() => setInviteDialogOpen(true)}>
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
                    {members?.items.map((member) => (
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
                              <p className="text-sm text-muted-foreground">
                                {member.user.email}
                              </p>
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
                    ))}
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
                    {invitations.items.map((invitation) => (
                      <TableRow key={invitation.id}>
                        <TableCell>{invitation.email}</TableCell>
                        <TableCell>
                          <Badge variant="outline" className="capitalize">
                            {invitation.role}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {formatRelativeTime(invitation.expiresAt)}
                        </TableCell>
                        <TableCell>
                          {canManageMembers && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => revokeInvitation.mutate(invitation.id)}
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
        </TabsContent>
      </Tabs>

      <InviteMemberDialog
        open={inviteDialogOpen}
        onOpenChange={setInviteDialogOpen}
        orgId={orgId}
      />
    </div>
  );
}
