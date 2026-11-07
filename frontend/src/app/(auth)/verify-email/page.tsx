"use client";

import * as React from "react";
import { Suspense } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { authApi, ApiClientError } from "@/lib/api";
import { CheckCircle, AlertCircle, Loader2 } from "lucide-react";

type VerificationStatus = "loading" | "success" | "error";

function VerifyEmailLoading() {
  return (
    <Card>
      <CardHeader className="space-y-1">
        <div className="flex justify-center mb-2">
          <Loader2 className="h-12 w-12 text-primary animate-spin" />
        </div>
        <CardTitle className="text-2xl text-center">Loading...</CardTitle>
      </CardHeader>
    </Card>
  );
}

function VerifyEmailContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get("token");

  const [status, setStatus] = React.useState<VerificationStatus>("loading");
  const [errorMessage, setErrorMessage] = React.useState<string>("");

  React.useEffect(() => {
    const verifyEmail = async () => {
      if (!token) {
        setStatus("error");
        setErrorMessage("No verification token provided");
        return;
      }

      try {
        await authApi.verifyEmail(token);
        setStatus("success");
      } catch (err) {
        setStatus("error");
        if (ApiClientError.isApiError(err)) {
          if (err.code === "TOKEN_EXPIRED") {
            setErrorMessage("This verification link has expired. Please request a new one.");
          } else if (err.code === "TOKEN_INVALID" || err.status === 404) {
            setErrorMessage("This verification link is invalid.");
          } else if (err.code === "ALREADY_VERIFIED") {
            setErrorMessage("This email has already been verified.");
          } else {
            setErrorMessage(err.message);
          }
        } else {
          setErrorMessage("An unexpected error occurred");
        }
      }
    };

    verifyEmail();
  }, [token]);

  if (status === "loading") {
    return (
      <Card>
        <CardHeader className="space-y-1">
          <div className="flex justify-center mb-2">
            <Loader2 className="h-12 w-12 text-primary animate-spin" />
          </div>
          <CardTitle className="text-2xl text-center">Verifying your email</CardTitle>
          <CardDescription className="text-center">
            Please wait while we verify your email address...
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  if (status === "success") {
    return (
      <Card>
        <CardHeader className="space-y-1">
          <div className="flex justify-center mb-2">
            <CheckCircle className="h-12 w-12 text-green-500" />
          </div>
          <CardTitle className="text-2xl text-center">Email verified</CardTitle>
          <CardDescription className="text-center">
            Your email address has been verified successfully. You can now log in to your account.
          </CardDescription>
        </CardHeader>
        <CardFooter className="flex flex-col space-y-4">
          <Link href="/login" className="w-full">
            <Button className="w-full">Go to login</Button>
          </Link>
        </CardFooter>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="space-y-1">
        <div className="flex justify-center mb-2">
          <AlertCircle className="h-12 w-12 text-destructive" />
        </div>
        <CardTitle className="text-2xl text-center">Verification failed</CardTitle>
        <CardDescription className="text-center">
          {errorMessage}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground text-center">
          If you need a new verification email, please log in and request one from your account settings.
        </p>
      </CardContent>
      <CardFooter className="flex flex-col space-y-4">
        <Link href="/login" className="w-full">
          <Button className="w-full">Go to login</Button>
        </Link>
      </CardFooter>
    </Card>
  );
}

export default function VerifyEmailPage() {
  return (
    <Suspense fallback={<VerifyEmailLoading />}>
      <VerifyEmailContent />
    </Suspense>
  );
}
