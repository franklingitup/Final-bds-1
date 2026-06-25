// Minimal typed API client skeleton. Generated client code (from the OpenAPI
// spec) should replace/extend this. No business logic implemented here.

const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

export interface ApiError {
  code: string;
  message: string;
  details?: string[];
  requestId?: string;
}

export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init.headers ?? {}),
    },
  });

  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as
      | { error: ApiError }
      | null;
    throw body?.error ?? { code: "INTERNAL", message: res.statusText };
  }

  return (await res.json()) as T;
}
