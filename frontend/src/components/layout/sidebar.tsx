"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname, useParams } from "next/navigation";
import {
  Building2,
  Server,
  FolderKanban,
  Key,
  Rocket,
  Box,
  Shield,
  Settings,
  ChevronDown,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useOrganization } from "@/providers/organization-provider";
import { useOrganizations } from "@/hooks/use-organizations";

interface NavItem {
  title: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
  requiresProject?: boolean;
}

const navItems: NavItem[] = [
  {
    title: "Projects",
    href: "/projects",
    icon: FolderKanban,
  },
  {
    title: "Clusters",
    href: "/clusters",
    icon: Server,
  },
  {
    title: "Deployments",
    href: "/deployments",
    icon: Rocket,
  },
  {
    title: "Audit Logs",
    href: "/audit",
    icon: Shield,
  },
  {
    title: "Settings",
    href: "/settings",
    icon: Settings,
  },
];

export function Sidebar() {
  const pathname = usePathname();
  const params = useParams();
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const { data: organizations } = useOrganizations();

  return (
    <div className="flex h-screen w-64 flex-col border-r bg-card">
      <div className="p-4 border-b">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" className="w-full justify-between">
              <div className="flex items-center gap-2">
                <Building2 className="h-4 w-4" />
                <span className="truncate">{organization?.name || "Select org"}</span>
              </div>
              <ChevronDown className="h-4 w-4 opacity-50" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-56" align="start">
            {organizations?.items.map((org) => (
              <DropdownMenuItem key={org.id} asChild>
                <Link href={`/${org.slug}/projects`}>
                  <Building2 className="mr-2 h-4 w-4" />
                  {org.name}
                </Link>
              </DropdownMenuItem>
            ))}
            <DropdownMenuItem asChild>
              <Link href="/organizations/new">
                <Building2 className="mr-2 h-4 w-4" />
                Create organization
              </Link>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <ScrollArea className="flex-1 px-3 py-4">
        <nav className="space-y-1">
          {navItems.map((item) => {
            const href = `/${orgSlug}${item.href}`;
            const isActive = pathname.startsWith(href);

            return (
              <Link
                key={item.href}
                href={href}
                className={cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                )}
              >
                <item.icon className="h-4 w-4" />
                {item.title}
              </Link>
            );
          })}
        </nav>
      </ScrollArea>
    </div>
  );
}
