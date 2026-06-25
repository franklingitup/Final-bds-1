"use client";

import * as React from "react";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { organizationsApi } from "@/lib/api";
import type { Organization, OrganizationMember } from "@/types/api";
import { useAuth } from "./auth-provider";

interface OrganizationContextValue {
  organization: Organization | null;
  membership: OrganizationMember | null;
  isLoading: boolean;
  isOwner: boolean;
  isAdmin: boolean;
  canManageMembers: boolean;
  canManageSettings: boolean;
}

const OrganizationContext = React.createContext<OrganizationContextValue | undefined>(undefined);

export function OrganizationProvider({ children }: { children: React.ReactNode }) {
  const params = useParams();
  const { user, isAuthenticated } = useAuth();
  const orgSlug = params.orgSlug as string | undefined;

  const { data: organization, isLoading: isOrgLoading } = useQuery({
    queryKey: ["organizations", "by-slug", orgSlug],
    queryFn: () => organizationsApi.getBySlug(orgSlug!),
    enabled: !!orgSlug && isAuthenticated,
  });

  const { data: membersData, isLoading: isMembersLoading } = useQuery({
    queryKey: ["organizations", organization?.id, "members"],
    queryFn: () => organizationsApi.listMembers(organization!.id),
    enabled: !!organization?.id,
  });

  const membership = React.useMemo(() => {
    if (!membersData?.items || !user) return null;
    return membersData.items.find((m) => m.userId === user.id) ?? null;
  }, [membersData, user]);

  const value: OrganizationContextValue = {
    organization: organization ?? null,
    membership,
    isLoading: isOrgLoading || isMembersLoading,
    isOwner: membership?.role === "owner",
    isAdmin: membership?.role === "owner" || membership?.role === "admin",
    canManageMembers: membership?.role === "owner" || membership?.role === "admin",
    canManageSettings: membership?.role === "owner" || membership?.role === "admin",
  };

  return (
    <OrganizationContext.Provider value={value}>
      {children}
    </OrganizationContext.Provider>
  );
}

export function useOrganization() {
  const context = React.useContext(OrganizationContext);
  if (context === undefined) {
    throw new Error("useOrganization must be used within an OrganizationProvider");
  }
  return context;
}
