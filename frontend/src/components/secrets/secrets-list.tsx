"use client";

import * as React from "react";
import { MoreHorizontal, Pencil, Trash2, Eye, EyeOff } from "lucide-react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Badge } from "@/components/ui/badge";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useDeleteSecret } from "@/hooks/use-secrets";
import type { Secret } from "@/types/api";
import { formatRelativeTime } from "@/lib/utils";
import { UpdateSecretDialog } from "./update-secret-dialog";

interface SecretsListProps {
  secrets: Secret[];
  orgId: string;
  projectId: string;
}

export function SecretsList({ secrets, orgId, projectId }: SecretsListProps) {
  const deleteSecret = useDeleteSecret(orgId, projectId);
  const [editingSecret, setEditingSecret] = React.useState<Secret | null>(null);

  return (
    <>
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Updated</TableHead>
              <TableHead className="w-[70px]"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {secrets.map((secret) => (
              <TableRow key={secret.id}>
                <TableCell className="font-mono font-medium">{secret.name}</TableCell>
                <TableCell className="text-muted-foreground">
                  {secret.description || "—"}
                </TableCell>
                <TableCell>
                  <Badge variant="secondary">v{secret.version}</Badge>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {formatRelativeTime(secret.updatedAt)}
                </TableCell>
                <TableCell>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon">
                        <MoreHorizontal className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => setEditingSecret(secret)}>
                        <Pencil className="mr-2 h-4 w-4" />
                        Update Value
                      </DropdownMenuItem>
                      <ConfirmDialog
                        trigger={
                          <DropdownMenuItem
                            onSelect={(e) => e.preventDefault()}
                            className="text-destructive"
                          >
                            <Trash2 className="mr-2 h-4 w-4" />
                            Delete
                          </DropdownMenuItem>
                        }
                        title="Delete Secret"
                        description={`Are you sure you want to delete "${secret.name}"? This action cannot be undone.`}
                        confirmLabel="Delete"
                        variant="destructive"
                        onConfirm={() => deleteSecret.mutateAsync(secret.id)}
                      />
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <UpdateSecretDialog
        secret={editingSecret}
        onClose={() => setEditingSecret(null)}
        orgId={orgId}
        projectId={projectId}
      />
    </>
  );
}
