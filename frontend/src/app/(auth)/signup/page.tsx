"use client";

import * as React from "react";
import Link from "next/link";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { FormInput } from "@/components/shared/form-field";
import { useAuth } from "@/providers/auth-provider";
import { ApiClientError } from "@/lib/api";

const signupSchema = z
  .object({
    name: z.string().min(2, "Name must be at least 2 characters"),
    email: z.string().email("Invalid email address"),
    password: z.string().min(8, "Password must be at least 8 characters"),
    confirmPassword: z.string(),
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: "Passwords don't match",
    path: ["confirmPassword"],
  });

type SignupForm = z.infer<typeof signupSchema>;

export default function SignupPage() {
  const { signup } = useAuth();
  const [error, setError] = React.useState<string | null>(null);

  const methods = useForm<SignupForm>({
    resolver: zodResolver(signupSchema),
    defaultValues: {
      name: "",
      email: "",
      password: "",
      confirmPassword: "",
    },
  });

  const {
    handleSubmit,
    formState: { isSubmitting },
  } = methods;

  const onSubmit = async (data: SignupForm) => {
    setError(null);
    try {
      await signup({
        name: data.name,
        email: data.email,
        password: data.password,
      });
    } catch (err) {
      if (ApiClientError.isApiError(err)) {
        setError(err.message);
      } else {
        setError("An unexpected error occurred");
      }
    }
  };

  return (
    <Card>
      <CardHeader className="space-y-1">
        <CardTitle className="text-2xl text-center">Create an account</CardTitle>
        <CardDescription className="text-center">
          Enter your details to get started
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
            <FormInput<SignupForm>
              name="name"
              label="Name"
              placeholder="John Doe"
              autoComplete="name"
              required
            />
            <FormInput<SignupForm>
              name="email"
              label="Email"
              type="email"
              placeholder="name@example.com"
              autoComplete="email"
              required
            />
            <FormInput<SignupForm>
              name="password"
              label="Password"
              type="password"
              autoComplete="new-password"
              required
            />
            <FormInput<SignupForm>
              name="confirmPassword"
              label="Confirm Password"
              type="password"
              autoComplete="new-password"
              required
            />
          </CardContent>
          <CardFooter className="flex flex-col space-y-4">
            <Button type="submit" className="w-full" loading={isSubmitting}>
              Create account
            </Button>
            <p className="text-sm text-muted-foreground text-center">
              Already have an account?{" "}
              <Link href="/login" className="text-primary hover:underline">
                Sign in
              </Link>
            </p>
          </CardFooter>
        </form>
      </FormProvider>
    </Card>
  );
}
