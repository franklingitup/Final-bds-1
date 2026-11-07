import { apiClient } from "./client";
import type { AuthTokens, User, LoginRequest, SignupRequest } from "@/types/api";

interface LoginResponse extends AuthTokens {
  user: User;
}

interface SignupResponse extends AuthTokens {
  user: User;
}

interface SSOExchangeRequest {
  orgId: string;
  code: string;
}

export interface MFASetupResponse {
  secret: string;
  otpauthUrl: string;
}

export const authApi = {
  async login(data: LoginRequest): Promise<LoginResponse> {
    const response = await apiClient.post<LoginResponse>("/v1/auth/login", data);
    apiClient.setTokens(response.accessToken, response.refreshToken);
    return response;
  },

  async signup(data: SignupRequest): Promise<SignupResponse> {
    const response = await apiClient.post<SignupResponse>("/v1/auth/signup", data);
    apiClient.setTokens(response.accessToken, response.refreshToken);
    return response;
  },

  async exchangeSSO(data: SSOExchangeRequest): Promise<LoginResponse> {
    const response = await apiClient.post<LoginResponse>("/v1/auth/sso/exchange", data);
    apiClient.setTokens(response.accessToken, response.refreshToken);
    return response;
  },

  // API-CRIT-09: Backend requires refreshToken in request body
  async logout(): Promise<void> {
    try {
      const refreshToken = typeof window !== "undefined" 
        ? localStorage.getItem("refreshToken") 
        : null;
      await apiClient.post("/v1/auth/logout", { refreshToken });
    } finally {
      apiClient.clearTokens();
    }
  },

  async getMe(): Promise<User> {
    return apiClient.get<User>("/v1/auth/me");
  },

  // API-MED-06: Backend requires refreshToken in request body  
  async refreshToken(): Promise<AuthTokens> {
    const refreshToken = typeof window !== "undefined" 
      ? localStorage.getItem("refreshToken") 
      : null;
    return apiClient.post<AuthTokens>("/v1/auth/refresh", { refreshToken });
  },

  // Password reset
  async requestPasswordReset(email: string): Promise<void> {
    await apiClient.post("/v1/auth/password-reset/request", { email });
  },

  async confirmPasswordReset(token: string, newPassword: string): Promise<void> {
    await apiClient.post("/v1/auth/password-reset/confirm", { token, newPassword });
  },

  // Email verification
  async verifyEmail(token: string): Promise<void> {
    await apiClient.post("/v1/auth/verify-email", { token });
  },

  // MFA/TOTP
  async setupMFA(): Promise<MFASetupResponse> {
    return apiClient.post<MFASetupResponse>("/v1/auth/mfa/setup");
  },

  async enableMFA(code: string): Promise<void> {
    await apiClient.post("/v1/auth/mfa/enable", { code });
  },

  async disableMFA(code: string): Promise<void> {
    await apiClient.post("/v1/auth/mfa/disable", { code });
  },
};
