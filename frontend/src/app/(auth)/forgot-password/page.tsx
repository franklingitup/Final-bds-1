"use client";

import * as React from "react";
import Link from "next/link";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { FormInput } from "@/components/shared/form-field";
import { authApi } from "@/lib/api";
import { CheckCircle } from "lucide-react";

const forgotPasswordSchema = z.object({
  email: z.string().email("Invalid email address"),
});

type ForgotPasswordForm = z.infer<typeof forgotPasswordSchema>;

export default function ForgotPasswordPage() {
  const [isSubmitted, setIsSubmitted] = React.useState(false);

  const methods = useForm<ForgotPasswordForm>({
    resolver: zodResolver(forgotPasswordSchema),
    defaultValues: {
      email: "",
    },
  });

  const {
    handleSubmit,
    formState: { isSubmitting },
  } = methods;

  const onSubmit = async (data: ForgotPasswordForm) => {
    // Always show success to prevent account enumeration
    // Backend already handles this, but we enforce it client-side too
    try {
      await authApi.requestPasswordReset(data.email);
    } catch {
      // Intentionally ignore errors to prevent enumeration
    }
    setIsSubmitted(true);
  };

  if (isSubmitted) {
    return (
      <Card>
        <CardHeader className="space-y-1">
          <div className="flex justify-center mb-2">
            <CheckCircle className="h-12 w-12 text-green-500" />
          </div>
          <CardTitle className="text-2xl text-center">Check your email</CardTitle>
          <CardDescription className="text-center">
            If that email address is registered, we&apos;ve sent password reset instructions to it.
            Please check your inbox and spam folder.
          </CardDescription>
        </CardHeader>
        <CardFooter className="flex flex-col space-y-4">
          <Link href="/login" className="w-full">
            <Button variant="outline" className="w-full">
              Back to login
            </Button>
          </Link>
        </CardFooter>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="space-y-1">
        <CardTitle className="text-2xl text-center">Forgot password?</CardTitle>
        <CardDescription className="text-center">
          Enter your email address and we&apos;ll send you a link to reset your password
        </CardDescription>
      </CardHeader>
      <FormProvider {...methods}>
        <form onSubmit={handleSubmit(onSubmit)}>
          <CardContent className="space-y-4">
            <FormInput<ForgotPasswordForm>
              name="email"
              label="Email"
              type="email"
              placeholder="name@example.com"
              autoComplete="email"
              required
            />
          </CardContent>
          <CardFooter className="flex flex-col space-y-4">
            <Button type="submit" className="w-full" loading={isSubmitting}>
              Send reset link
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
