"use client";

import * as React from "react";
import { Suspense } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { FormInput } from "@/components/shared/form-field";
import { authApi, ApiClientError } from "@/lib/api";
import { CheckCircle, AlertCircle, Loader2 } from "lucide-react";

const resetPasswordSchema = z
  .object({
    password: z.string().min(8, "Password must be at least 8 characters"),
    confirmPassword: z.string(),
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: "Passwords don't match",
    path: ["confirmPassword"],
  });

type ResetPasswordForm = z.infer<typeof resetPasswordSchema>;

function ResetPasswordLoading() {
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

function ResetPasswordContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const token = searchParams.get("token");

  const [error, setError] = React.useState<string | null>(null);
  const [isSuccess, setIsSuccess] = React.useState(false);
  const [isInvalidToken, setIsInvalidToken] = React.useState(false);

  const methods = useForm<ResetPasswordForm>({
    resolver: zodResolver(resetPasswordSchema),
    defaultValues: {
      password: "",
      confirmPassword: "",
    },
  });

  const {
    handleSubmit,
    formState: { isSubmitting },
  } = methods;

  // Check for missing token on mount
  React.useEffect(() => {
    if (!token) {
      setIsInvalidToken(true);
    }
  }, [token]);

  const onSubmit = async (data: ResetPasswordForm) => {
    if (!token) {
      setIsInvalidToken(true);
      return;
    }

    setError(null);
    try {
      await authApi.confirmPasswordReset(token, data.password);
      setIsSuccess(true);
      // Redirect to login after 3 seconds
      setTimeout(() => {
        router.push("/login?message=password_reset_success");
      }, 3000);
    } catch (err) {
      if (ApiClientError.isApiError(err)) {
        // Check for token-related errors
        if (err.code === "TOKEN_EXPIRED" || err.code === "TOKEN_INVALID" || err.status === 404) {
          setIsInvalidToken(true);
        } else {
          setError(err.message);
        }
      } else {
        setError("An unexpected error occurred");
      }
    }
  };

  if (isInvalidToken) {
    return (
      <Card>
        <CardHeader className="space-y-1">
          <div className="flex justify-center mb-2">
            <AlertCircle className="h-12 w-12 text-destructive" />
          </div>
          <CardTitle className="text-2xl text-center">Invalid or expired link</CardTitle>
          <CardDescription className="text-center">
            This password reset link is invalid or has expired. Please request a new one.
          </CardDescription>
        </CardHeader>
        <CardFooter className="flex flex-col space-y-4">
          <Link href="/forgot-password" className="w-full">
            <Button className="w-full">Request new reset link</Button>
          </Link>
          <Link
            href="/login"
            className="text-sm text-muted-foreground hover:text-primary text-center"
          >
            Back to login
          </Link>
        </CardFooter>
      </Card>
    );
  }

  if (isSuccess) {
    return (
      <Card>
        <CardHeader className="space-y-1">
          <div className="flex justify-center mb-2">
            <CheckCircle className="h-12 w-12 text-green-500" />
          </div>
          <CardTitle className="text-2xl text-center">Password reset successful</CardTitle>
          <CardDescription className="text-center">
            Your password has been reset successfully. You will be redirected to the login page.
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
        <CardTitle className="text-2xl text-center">Reset your password</CardTitle>
        <CardDescription className="text-center">
          Enter your new password below
        </CardDescription>
      </CardHeader>
      <FormProvider {...methods}>
        <form onSubmit={handleSubmit(onSubmit)}>
          <CardContent className="space-y-4">
            {error && (
              <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
                {error}
              </div>
            )}
            <FormInput<ResetPasswordForm>
              name="password"
              label="New Password"
              type="password"
              autoComplete="new-password"
              required
            />
            <FormInput<ResetPasswordForm>
              name="confirmPassword"
              label="Confirm New Password"
              type="password"
              autoComplete="new-password"
              required
            />
          </CardContent>
          <CardFooter className="flex flex-col space-y-4">
            <Button type="submit" className="w-full" loading={isSubmitting}>
              Reset password
            </Button>
            <Link
              href="/login"
              className="text-sm text-muted-foreground hover:text-primary text-center"
            >
              Back to login
            </Link>
          </CardFooter>
        </form>
      </FormProvider>
    </Card>
  );
}

export default function ResetPasswordPage() {
  return (
    <Suspense fallback={<ResetPasswordLoading />}>
      <ResetPasswordContent />
    </Suspense>
  );
}
