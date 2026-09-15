"use client";

import * as React from "react";
import { useSearchParams } from "next/navigation";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { authApi } from "@/lib/api";

function SSOCallbackContent() {
  const searchParams = useSearchParams();
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    const code = searchParams.get("code");
    const orgId = searchParams.get("orgId");
    if (!code || !orgId) {
      setError("The SSO response is missing its exchange code.");
      return;
    }

    let cancelled = false;
    void authApi.exchangeSSO({ code, orgId })
      .then(() => {
        if (!cancelled) {
          // Reload so AuthProvider initializes from the newly stored tokens.
          window.location.replace("/");
        }
      })
      .catch(() => {
        if (!cancelled) {
          setError("SSO sign-in failed or the exchange code expired. Please try again.");
        }
      });

    return () => {
      cancelled = true;
    };
  }, [searchParams]);

  return (
    <Card>
      <CardHeader className="space-y-1">
        <CardTitle className="text-2xl text-center">Signing you in</CardTitle>
        <CardDescription className="text-center">
          Completing your organization&apos;s SSO login.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {error ? (
          <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
            {error}
          </div>
        ) : (
          <p className="text-center text-sm text-muted-foreground">Please wait…</p>
        )}
      </CardContent>
    </Card>
  );
}

export default function SSOCallbackPage() {
  return (
    <React.Suspense
      fallback={
        <Card>
          <CardHeader className="space-y-1">
            <CardTitle className="text-2xl text-center">Signing you in</CardTitle>
            <CardDescription className="text-center">
              Completing your organization&apos;s SSO login.
            </CardDescription>
          </CardHeader>
        </Card>
      }
    >
      <SSOCallbackContent />
    </React.Suspense>
  );
}
