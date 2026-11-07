"use client";

import * as React from "react";
import Link from "next/link";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { ArrowLeft, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FormInput } from "@/components/shared/form-field";
import { useAuth } from "@/providers/auth-provider";
import { ApiClientError } from "@/lib/api";

const loginSchema = z.object({
  email: z.string().email("Invalid email address"),
  password: z.string().min(1, "Password is required"),
});

type LoginForm = z.infer<typeof loginSchema>;

export default function LoginPage() {
  const { login } = useAuth();
  const [error, setError] = React.useState<string | null>(null);
  const [mfaRequired, setMfaRequired] = React.useState(false);
  const [mfaCode, setMfaCode] = React.useState("");
  const [savedCredentials, setSavedCredentials] = React.useState<LoginForm | null>(null);
  const [isMfaSubmitting, setIsMfaSubmitting] = React.useState(false);

  const methods = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: {
      email: "",
      password: "",
    },
  });

  const {
    handleSubmit,
    formState: { isSubmitting },
  } = methods;

  const onSubmit = async (data: LoginForm) => {
    setError(null);
    try {
      await login(data);
    } catch (err) {
      if (ApiClientError.isApiError(err)) {
        if (err.code === "MFA_REQUIRED") {
          // Save credentials and show MFA input
          setSavedCredentials(data);
          setMfaRequired(true);
          setError(null);
        } else {
          setError(err.message);
        }
      } else {
        setError("An unexpected error occurred");
      }
    }
  };

  const onMfaSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!savedCredentials || !mfaCode) return;

    setError(null);
    setIsMfaSubmitting(true);
    try {
      await login({ ...savedCredentials, mfaCode });
    } catch (err) {
      if (ApiClientError.isApiError(err)) {
        setError(err.message);
      } else {
        setError("An unexpected error occurred");
      }
    } finally {
      setIsMfaSubmitting(false);
    }
  };

  const handleBackToLogin = () => {
    setMfaRequired(false);
    setMfaCode("");
    setSavedCredentials(null);
    setError(null);
  };

  // MFA code entry step
  if (mfaRequired) {
    return (
      <Card>
        <CardHeader className="space-y-1">
          <div className="flex justify-center mb-2">
            <Shield className="h-12 w-12 text-primary" />
          </div>
          <CardTitle className="text-2xl text-center">Two-Factor Authentication</CardTitle>
          <CardDescription className="text-center">
            Enter the 6-digit code from your authenticator app
          </CardDescription>
        </CardHeader>
        <form onSubmit={onMfaSubmit}>
          <CardContent className="space-y-4">
            {error && (
              <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
                {error}
              </div>
            )}
            <div className="space-y-2">
              <Label htmlFor="mfa-code">Authentication Code</Label>
              <Input
                id="mfa-code"
                type="text"
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={6}
                placeholder="000000"
                value={mfaCode}
                onChange={(e) => setMfaCode(e.target.value.replace(/\D/g, ""))}
                className="font-mono text-center text-2xl tracking-[0.5em] h-14"
                autoFocus
              />
            </div>
          </CardContent>
          <CardFooter className="flex flex-col space-y-4">
            <Button 
              type="submit" 
              className="w-full" 
              loading={isMfaSubmitting}
              disabled={mfaCode.length !== 6}
            >
              Verify
            </Button>
            <button
              type="button"
              onClick={handleBackToLogin}
              className="flex items-center justify-center gap-2 text-sm text-muted-foreground hover:text-primary"
            >
              <ArrowLeft className="h-4 w-4" />
              Back to login
            </button>
          </CardFooter>
        </form>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="space-y-1">
        <CardTitle className="text-2xl text-center">Welcome back</CardTitle>
        <CardDescription className="text-center">
          Enter your credentials to sign in to your account
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
            <FormInput<LoginForm>
              name="email"
              label="Email"
              type="email"
              placeholder="name@example.com"
              autoComplete="email"
              required
            />
            <FormInput<LoginForm>
              name="password"
              label="Password"
              type="password"
              autoComplete="current-password"
              required
            />
            <div className="flex items-center justify-end">
              <Link
                href="/forgot-password"
                className="text-sm text-primary hover:underline"
              >
                Forgot password?
              </Link>
            </div>
          </CardContent>
          <CardFooter className="flex flex-col space-y-4">
            <Button type="submit" className="w-full" loading={isSubmitting}>
              Sign in
            </Button>
            <p className="text-sm text-muted-foreground text-center">
              Don&apos;t have an account?{" "}
              <Link href="/signup" className="text-primary hover:underline">
                Sign up
              </Link>
            </p>
          </CardFooter>
        </form>
      </FormProvider>
    </Card>
  );
}
