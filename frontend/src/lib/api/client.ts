import type { ApiError } from "@/types/api";

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "";

export class ApiClientError extends Error {
  constructor(
    public status: number,
    public code: string | undefined,
    message: string,
    public details?: Record<string, string[]>
  ) {
    super(message);
    this.name = "ApiClientError";
  }

  static isApiError(error: unknown): error is ApiClientError {
    return error instanceof ApiClientError;
  }
}

interface RequestOptions extends RequestInit {
  params?: Record<string, string | number | boolean | undefined>;
}

class ApiClient {
  private accessToken: string | null = null;
  private refreshToken: string | null = null;
  private refreshPromise: Promise<void> | null = null;
  private onAuthError?: () => void;

  setTokens(accessToken: string, refreshToken: string) {
    this.accessToken = accessToken;
    this.refreshToken = refreshToken;
    if (typeof window !== "undefined") {
      localStorage.setItem("accessToken", accessToken);
      localStorage.setItem("refreshToken", refreshToken);
    }
  }

  clearTokens() {
    this.accessToken = null;
    this.refreshToken = null;
    if (typeof window !== "undefined") {
      localStorage.removeItem("accessToken");
      localStorage.removeItem("refreshToken");
    }
  }

  loadTokens() {
    if (typeof window !== "undefined") {
      this.accessToken = localStorage.getItem("accessToken");
      this.refreshToken = localStorage.getItem("refreshToken");
    }
  }

  getAccessToken(): string | null {
    return this.accessToken;
  }

  hasTokens(): boolean {
    return !!this.accessToken && !!this.refreshToken;
  }

  setAuthErrorHandler(handler: () => void) {
    this.onAuthError = handler;
  }

  private async refreshAccessToken(): Promise<void> {
    if (!this.refreshToken) {
      throw new ApiClientError(401, "NO_TOKEN", "No refresh token available");
    }

    const response = await fetch(`${API_BASE_URL}/v1/auth/refresh`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ refreshToken: this.refreshToken }),
    });

    if (!response.ok) {
      this.clearTokens();
      this.onAuthError?.();
      throw new ApiClientError(401, "REFRESH_FAILED", "Failed to refresh token");
    }

    const data = await response.json();
    this.setTokens(data.accessToken, data.refreshToken);
  }

  private async ensureValidToken(): Promise<void> {
    if (!this.accessToken) {
      this.loadTokens();
    }

    if (!this.accessToken) {
      return;
    }

    try {
      const payload = JSON.parse(atob(this.accessToken.split(".")[1]));
      const expiresAt = payload.exp * 1000;
      const now = Date.now();
      const buffer = 60 * 1000; // 1 minute buffer

      if (expiresAt - now < buffer) {
        if (!this.refreshPromise) {
          this.refreshPromise = this.refreshAccessToken().finally(() => {
            this.refreshPromise = null;
          });
        }
        await this.refreshPromise;
      }
    } catch {
      // Invalid token format, let the request proceed and handle 401
    }
  }

  private buildUrl(path: string, params?: Record<string, string | number | boolean | undefined>): string {
    const url = new URL(`${API_BASE_URL}${path}`);
    if (params) {
      Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined) {
          url.searchParams.append(key, String(value));
        }
      });
    }
    return url.toString();
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    await this.ensureValidToken();

    const { params, ...fetchOptions } = options;
    const url = this.buildUrl(path, params);

    const headers: HeadersInit = {
      "Content-Type": "application/json",
      ...options.headers,
    };

    if (this.accessToken) {
      (headers as Record<string, string>)["Authorization"] = `Bearer ${this.accessToken}`;
    }

    const response = await fetch(url, {
      ...fetchOptions,
      headers,
    });

    if (!response.ok) {
      if (response.status === 401) {
        // Try to refresh once
        try {
          await this.refreshAccessToken();
          // Retry the request
          if (this.accessToken) {
            (headers as Record<string, string>)["Authorization"] = `Bearer ${this.accessToken}`;
          }
          const retryResponse = await fetch(url, {
            ...fetchOptions,
            headers,
          });
          if (retryResponse.ok) {
            const contentType = retryResponse.headers.get("content-type");
            if (contentType?.includes("application/json")) {
              return retryResponse.json();
            }
            return {} as T;
          }
        } catch {
          this.onAuthError?.();
          throw new ApiClientError(401, "UNAUTHORIZED", "Authentication required");
        }
      }

      // API-CRIT-05: Backend returns nested error envelope { error: { code, message, details } }
      let errorData: ApiError;
      try {
        errorData = await response.json();
      } catch {
        errorData = { error: { code: "UNKNOWN", message: response.statusText } };
      }

      throw new ApiClientError(
        response.status,
        errorData.error?.code,
        errorData.error?.message || response.statusText,
        errorData.error?.details ? { general: errorData.error.details } : undefined
      );
    }

    const contentType = response.headers.get("content-type");
    if (contentType?.includes("application/json")) {
      return response.json();
    }
    return {} as T;
  }

  async get<T>(path: string, params?: Record<string, string | number | boolean | undefined>): Promise<T> {
    return this.request<T>(path, { method: "GET", params });
  }

  async post<T>(path: string, data?: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "POST",
      body: data ? JSON.stringify(data) : undefined,
    });
  }

  async patch<T>(path: string, data?: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "PATCH",
      body: data ? JSON.stringify(data) : undefined,
    });
  }

  async delete<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: "DELETE" });
  }
}

export const apiClient = new ApiClient();
