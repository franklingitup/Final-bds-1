"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/providers/auth-provider";
import { useOrganizations } from "@/hooks/use-organizations";
import { LoadingState } from "@/components/shared/loading-state";

export default function HomePage() {
  const router = useRouter();
  const { isAuthenticated, isLoading: isAuthLoading } = useAuth();
  const { data: organizations, isLoading: isOrgsLoading } = useOrganizations();

  useEffect(() => {
    if (isAuthLoading || isOrgsLoading) return;

    if (!isAuthenticated) {
      router.push("/login");
      return;
    }

    if (organizations?.items && organizations.items.length > 0) {
      router.push(`/${organizations.items[0].slug}/projects`);
    } else {
      router.push("/organizations/new");
    }
  }, [isAuthenticated, isAuthLoading, organizations, isOrgsLoading, router]);

  return (
    <div className="flex min-h-screen items-center justify-center">
      <LoadingState message="Loading your workspace..." />
    </div>
  );
}
