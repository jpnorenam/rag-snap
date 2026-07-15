import { getSync } from "./envelope";

// ServerInfo is the metadata returned by GET /1.0 (and the subset of GET /
// the UI cares about): the API version, advertised extensions, and the
// caller's auth state.
export interface ServerInfo {
  api_version: string;
  api_extensions: string[];
  auth: string;
  backends?: Record<string, unknown>;
  config?: Record<string, unknown>;
}

// getApiRoot fetches GET / (discovery; reachable untrusted). Used to confirm
// the daemon is reachable and whether the caller is authenticated.
export function getApiRoot(): Promise<ServerInfo> {
  return getSync<ServerInfo>("/");
}

// getServerInfo fetches GET /1.0 (authenticated server info).
export function getServerInfo(): Promise<ServerInfo> {
  return getSync<ServerInfo>("/1.0");
}

// knowledgeInitialized reports whether the knowledge engine has been initialized,
// read from the server info config summary (config.knowledge.initialized).
export async function knowledgeInitialized(): Promise<boolean> {
  const info = await getServerInfo();
  const cfg = info.config as { knowledge?: { initialized?: boolean } } | undefined;
  return cfg?.knowledge?.initialized === true;
}
