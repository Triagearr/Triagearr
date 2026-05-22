import { z } from "zod";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/**
 * apiFetch issues a same-origin request, attaching the session cookie via
 * `credentials: 'include'`. Authentication is opt-in (see ADR-0019): when
 * the daemon has no registered user the cookie is unnecessary; once it
 * does, the cookie is set by the login flow and every subsequent request
 * carries it automatically.
 */
async function request(path: string, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers ?? {});
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(path, { ...init, headers, credentials: "include" });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const body = (await res.json()) as { error?: string };
      if (body?.error) detail = body.error;
    } catch {
      /* ignore body parse */
    }
    throw new ApiError(res.status, detail);
  }
  return res;
}

export async function apiFetch<T>(
  path: string,
  schema: z.ZodType<T>,
  init?: RequestInit,
): Promise<T> {
  const res = await request(path, init);
  const json = (await res.json()) as unknown;
  return schema.parse(json);
}

/** apiFetchVoid is apiFetch for endpoints that return no body (204, 202). */
export async function apiFetchVoid(path: string, init?: RequestInit): Promise<void> {
  await request(path, init);
}
