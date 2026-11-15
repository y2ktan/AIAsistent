export interface AuthUser {
  id: string;
  email: string;
  role: "user" | "admin";
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

let inMemoryCsrfToken = "";

export function setCsrfToken(token: string) {
  inMemoryCsrfToken = token;
}

export function getCsrfToken(): string {
  return inMemoryCsrfToken;
}

export async function apiFetch<T = unknown>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const method = (options.method || "GET").toUpperCase();
  const headers = new Headers(options.headers);

  if (method !== "GET" && method !== "HEAD") {
    if (inMemoryCsrfToken) {
      headers.set("X-CSRF-Token", inMemoryCsrfToken);
    }
    if (!(options.body instanceof FormData) && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
  }

  const res = await fetch(path, {
    ...options,
    headers,
    credentials: "same-origin",
  });

  const text = await res.text();
  let data: Record<string, unknown> = {};
  if (text) {
    try {
      data = JSON.parse(text) as Record<string, unknown>;
    } catch {
      data = { error: text };
    }
  }

  if (!res.ok) {
    const msg =
      typeof data.error === "string" ? data.error : `Request failed (${res.status})`;
    throw new Error(msg);
  }

  return data as T;
}

export async function fetchSession(): Promise<{
  user: AuthUser;
  csrfToken: string;
} | null> {
  try {
    const data = await apiFetch<{ user: AuthUser; csrfToken: string }>(
      "/api/v1/auth/me"
    );
    if (data.csrfToken) {
      setCsrfToken(data.csrfToken);
    }
    return data;
  } catch {
    return null;
  }
}
