"use client";

import * as React from "react";
import { useParams, useSearchParams, useRouter } from "next/navigation";
import { Search, Filter, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { useAuditLogs } from "@/hooks/use-audit";
import { useOrganization } from "@/providers/organization-provider";
import { formatDate, formatRelativeTime } from "@/lib/utils";

const RESOURCE_TYPES = [
  { value: "", label: "All Resources" },
  { value: "organization", label: "Organization" },
  { value: "project", label: "Project" },
  { value: "cluster", label: "Cluster" },
  { value: "deployment", label: "Deployment" },
  { value: "secret", label: "Secret" },
  { value: "application", label: "Application" },
];

const ACTIONS = [
  { value: "", label: "All Actions" },
  { value: "created", label: "Created" },
  { value: "updated", label: "Updated" },
  { value: "deleted", label: "Deleted" },
  { value: "registered", label: "Registered" },
  { value: "deployed", label: "Deployed" },
];

export default function AuditPage() {
  const params = useParams();
  const searchParams = useSearchParams();
  const router = useRouter();
  const { organization } = useOrganization();

  const resourceType = searchParams.get("resourceType") || "";
  const action = searchParams.get("action") || "";

  const { data, isLoading, error, refetch } = useAuditLogs(organization?.id || "", {
    resourceType: resourceType || undefined,
    action: action || undefined,
    limit: 50,
  });

  const updateFilter = (key: string, value: string) => {
    const params = new URLSearchParams(searchParams.toString());
    if (value) {
      params.set(key, value);
    } else {
      params.delete(key);
    }
    router.push(`?${params.toString()}`);
  };

  if (isLoading) {
    return <LoadingState message="Loading audit logs..." />;
  }

  if (error) {
    return <ErrorState onRetry={() => refetch()} />;
  }

  const logs = data?.items || [];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Audit Logs"
        description="Track all actions in your organization"
      />

      <Card>
        <CardHeader>
          <div className="flex items-center gap-4">
            <Select value={resourceType} onValueChange={(v) => updateFilter("resourceType", v)}>
              <SelectTrigger className="w-[200px]">
                <SelectValue placeholder="All Resources" />
              </SelectTrigger>
              <SelectContent>
                {RESOURCE_TYPES.map((type) => (
                  <SelectItem key={type.value} value={type.value}>
                    {type.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select value={action} onValueChange={(v) => updateFilter("action", v)}>
              <SelectTrigger className="w-[200px]">
                <SelectValue placeholder="All Actions" />
              </SelectTrigger>
              <SelectContent>
                {ACTIONS.map((act) => (
                  <SelectItem key={act.value} value={act.value}>
                    {act.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </CardHeader>
        <CardContent>
          {logs.length === 0 ? (
            <EmptyState
              icon={<Shield className="h-6 w-6" />}
              title="No audit logs found"
              description="Audit logs will appear here as actions are performed"
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Action</TableHead>
                  <TableHead>Resource</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead>Time</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {logs.map((log) => (
                  <TableRow key={log.id}>
                    <TableCell>
                      <Badge variant="outline" className="capitalize">
                        {log.action}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div>
                        <span className="font-medium capitalize">
                          {log.resourceType}
                        </span>
                        {log.resourceName && (
                          <span className="text-muted-foreground ml-2">
                            {log.resourceName}
                          </span>
                        )}
                      </div>
                      <span className="text-xs text-muted-foreground font-mono">
                        {log.resourceId}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div>
                        <span className="font-medium">
                          {log.actorName || log.actorId}
                        </span>
                      </div>
                      <span className="text-xs text-muted-foreground capitalize">
                        {log.actorType}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div className="text-sm">
                        {formatRelativeTime(log.createdAt)}
                      </div>
                      <span className="text-xs text-muted-foreground">
                        {formatDate(log.createdAt)}
                      </span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
