import Link from "next/link";
import { Box } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { Application } from "@/types/api";
import { formatRelativeTime } from "@/lib/utils";

interface ApplicationCardProps {
  application: Application;
  orgSlug: string;
  projectSlug: string;
}

export function ApplicationCard({ application, orgSlug, projectSlug }: ApplicationCardProps) {
  return (
    <Link href={`/${orgSlug}/projects/${projectSlug}/applications/${application.slug}`}>
      <Card className="cursor-pointer transition-colors hover:bg-accent/50">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2">
              <Box className="h-5 w-5 text-muted-foreground" />
              {application.name}
            </CardTitle>
            <Badge variant="secondary">{application.runtimeType}</Badge>
          </div>
          {application.description && (
            <CardDescription className="line-clamp-2">
              {application.description}
            </CardDescription>
          )}
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            Created {formatRelativeTime(application.createdAt)}
          </p>
        </CardContent>
      </Card>
    </Link>
  );
}
