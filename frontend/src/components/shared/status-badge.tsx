import { Badge, type BadgeProps } from "@/components/ui/badge";

type Status =
  | "pending"
  | "connected"
  | "disconnected"
  | "deleted"
  | "running"
  | "succeeded"
  | "failed"
  | "deploying"
  | "rolled_back"
  | "active"
  | "used"
  | "expired"
  | "revoked";

const statusConfig: Record<Status, { label: string; variant: BadgeProps["variant"] }> = {
  pending: { label: "Pending", variant: "secondary" },
  connected: { label: "Connected", variant: "success" },
  disconnected: { label: "Disconnected", variant: "warning" },
  deleted: { label: "Deleted", variant: "destructive" },
  running: { label: "Running", variant: "success" },
  succeeded: { label: "Succeeded", variant: "success" },
  failed: { label: "Failed", variant: "destructive" },
  deploying: { label: "Deploying", variant: "default" },
  rolled_back: { label: "Rolled Back", variant: "warning" },
  active: { label: "Active", variant: "success" },
  used: { label: "Used", variant: "secondary" },
  expired: { label: "Expired", variant: "warning" },
  revoked: { label: "Revoked", variant: "destructive" },
};

interface StatusBadgeProps {
  status: string;
}

export function StatusBadge({ status }: StatusBadgeProps) {
  const config = statusConfig[status as Status] || {
    label: status,
    variant: "secondary" as const,
  };

  return <Badge variant={config.variant}>{config.label}</Badge>;
}

export function ClusterStatusBadge({ status }: StatusBadgeProps) {
  return <StatusBadge status={status} />;
}

export function DeploymentStatusBadge({ status }: StatusBadgeProps) {
  return <StatusBadge status={status} />;
}

export function ReleaseStatusBadge({ status }: StatusBadgeProps) {
  return <StatusBadge status={status} />;
}
