"use client";

import { Sidebar } from "@/components/layout/sidebar";
import { OrganizationProvider } from "@/providers/organization-provider";

export default function OrgLayout({ children }: { children: React.ReactNode }) {
  return (
    <OrganizationProvider>
      <div className="flex h-[calc(100vh-3.5rem)]">
        <Sidebar />
        <main className="flex-1 overflow-y-auto p-6">{children}</main>
      </div>
    </OrganizationProvider>
  );
}
