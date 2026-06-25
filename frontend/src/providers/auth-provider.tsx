"use client";

import * as React from "react";
import { useRouter, usePathname } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { authApi, apiClient } from "@/lib/api";
import type { User, LoginRequest, SignupRequest } from "@/types/api";

interface AuthContextValue {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (data: LoginRequest) => Promise<void>;
  signup: (data: SignupRequest) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = React.createContext<AuthContextValue | undefined>(undefined);

const PUBLIC_PATHS = ["/login", "/signup", "/forgot-password", "/reset-password"];

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const queryClient = useQueryClient();
  const [isInitialized, setIsInitialized] = React.useState(false);

  // Load tokens on mount
  React.useEffect(() => {
    apiClient.loadTokens();
    setIsInitialized(true);
  }, []);

  // Set up auth error handler
  React.useEffect(() => {
    apiClient.setAuthErrorHandler(() => {
      queryClient.setQueryData(["auth", "me"], null);
      if (!PUBLIC_PATHS.includes(pathname)) {
        router.push("/login");
      }
    });
  }, [queryClient, router, pathname]);

  const { data: user, isLoading: isUserLoading } = useQuery({
    queryKey: ["auth", "me"],
    queryFn: () => authApi.getMe(),
    enabled: isInitialized && apiClient.hasTokens(),
    retry: false,
    staleTime: 5 * 60 * 1000,
  });

  const loginMutation = useMutation({
    mutationFn: authApi.login,
    onSuccess: (data) => {
      queryClient.setQueryData(["auth", "me"], data.user);
      router.push("/");
    },
  });

  const signupMutation = useMutation({
    mutationFn: authApi.signup,
    onSuccess: (data) => {
      queryClient.setQueryData(["auth", "me"], data.user);
      router.push("/");
    },
  });

  const logoutMutation = useMutation({
    mutationFn: authApi.logout,
    onSuccess: () => {
      queryClient.clear();
      router.push("/login");
    },
  });

  // Redirect logic
  React.useEffect(() => {
    if (!isInitialized || isUserLoading) return;

    const isPublicPath = PUBLIC_PATHS.includes(pathname);
    const hasTokens = apiClient.hasTokens();

    if (!hasTokens && !isPublicPath) {
      router.push("/login");
    } else if (hasTokens && user && isPublicPath) {
      router.push("/");
    }
  }, [isInitialized, isUserLoading, user, pathname, router]);

  const isLoading = !isInitialized || isUserLoading;

  const value: AuthContextValue = {
    user: user ?? null,
    isLoading,
    isAuthenticated: !!user,
    login: async (data) => {
      await loginMutation.mutateAsync(data);
    },
    signup: async (data) => {
      await signupMutation.mutateAsync(data);
    },
    logout: async () => {
      await logoutMutation.mutateAsync();
    },
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = React.useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
