import { ROOT_PATH } from "./rootPath";
import { authHeaders, captureTokenFromUrl } from "./token";

// The daemon's uniform response envelope (LXD-style): every JSON response is a
// "sync", "async", or "error" object. This is the browser analogue of lxd-ui's
// handleResponse / the Go apiclient envelope.
export interface ApiEnvelope<T = unknown> {
  type: "sync" | "async" | "error";
  status?: string;
  status_code?: number;
  error_code?: number;
  error?: string;
  operation?: string;
  metadata?: T;
}

// ApiError is the typed error raised on an `error` envelope (or a transport/
// HTTP failure), carrying the status code and message for display.
export class ApiError extends Error {
  code: number;
  constructor(message: string, code: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
  }
}

// DAEMON_UNREACHABLE is the standard copy for a dead daemon (foundation §7).
// `ApiError.code === 0` means the request never reached ragd, so the raw
// transport message ("network error contacting the API: TypeError…") tells the
// user nothing actionable — this says what happened and what to do next.
export const DAEMON_UNREACHABLE =
  "Cannot reach the RAG daemon. Check that the service is running (`snap services rag-cli`).";

// errorMessage turns any thrown value into the sentence to show the user: the
// standard connection error when the daemon is unreachable, the daemon's own
// message otherwise. Every screen should render errors through this.
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    return e.code === 0 ? DAEMON_UNREACHABLE : e.message;
  }
  return e instanceof Error ? e.message : String(e);
}

// apiUrl builds an absolute-from-origin API path: `${ROOT_PATH}/1.0/...`.
export function apiUrl(path: string): string {
  const suffix = path.startsWith("/") ? path : `/${path}`;
  return `${ROOT_PATH}${suffix}`;
}

// request performs a fetch against the API and parses the envelope. The token
// is injected at runtime (Authorization header when present; the loopback
// cookie travels automatically via credentials: "include").
async function request<T>(
  method: string,
  path: string,
  body?: unknown
): Promise<ApiEnvelope<T>> {
  // Idempotent, and a no-op on the usual cookie path. It guarantees a fragment
  // token is picked up even when a caller fires before AppShell's mount-time
  // capture: child effects (OperationsProvider's seed) run before the parent's.
  captureTokenFromUrl();
  const headers: Record<string, string> = { ...authHeaders() };
  if (body !== undefined) headers["Content-Type"] = "application/json";

  let resp: Response;
  try {
    resp = await fetch(apiUrl(path), {
      method,
      headers,
      credentials: "include",
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    throw new ApiError(`network error contacting the API: ${String(e)}`, 0);
  }

  let env: ApiEnvelope<T>;
  try {
    env = (await resp.json()) as ApiEnvelope<T>;
  } catch {
    throw new ApiError(`unexpected non-JSON response (HTTP ${resp.status})`, resp.status);
  }

  if (env.type === "error") {
    throw new ApiError(env.error ?? `request failed`, env.error_code ?? resp.status);
  }
  return env;
}

// getSync issues a request expecting a sync response and returns its metadata.
export async function getSync<T>(path: string): Promise<T> {
  const env = await request<T>("GET", path);
  return env.metadata as T;
}

// postSync issues a request expecting a sync response and returns its metadata.
export async function postSync<T>(path: string, body?: unknown): Promise<T> {
  const env = await request<T>("POST", path, body);
  return env.metadata as T;
}

// deleteSync issues a DELETE expecting a sync response and returns its metadata.
export async function deleteSync<T>(path: string): Promise<T> {
  const env = await request<T>("DELETE", path);
  return env.metadata as T;
}

// postAsync issues a request expecting an async response and returns the
// operation object from metadata along with its canonical operation URL.
export async function postAsync<T>(
  path: string,
  body?: unknown
): Promise<{ operation: string; metadata: T }> {
  const env = await request<T>("POST", path, body);
  if (!env.operation) {
    throw new ApiError(`expected an async operation but got a "${env.type}" response`, 0);
  }
  return { operation: env.operation, metadata: env.metadata as T };
}
