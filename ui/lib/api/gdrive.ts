import { getSync, postAsync, postSync } from "./envelope";
import type { OperationView } from "./operations";

// GdriveStatus mirrors the daemon's gdriveStatusView: whether Drive import is
// configured, whether a token is stored, whether an OAuth flow is pending, plus
// the connected account and last-flow error when present.
export interface GdriveStatus {
  configured: boolean;
  connected: boolean;
  pending: boolean;
  account?: string;
  error?: string;
}

// GdriveArchive is one discovered .tar.gz archive.
export interface GdriveArchive {
  id: string;
  name: string;
  size: number;
  modified?: string;
}

// GdriveResolveResult is the outcome of resolving a Drive URL.
export interface GdriveResolveResult {
  kind: "file" | "folder";
  archives: GdriveArchive[];
}

// gdriveStatus reads the current Drive connection status.
export async function gdriveStatus(): Promise<GdriveStatus> {
  return getSync<GdriveStatus>("/1.0/knowledge/gdrive/status");
}

// gdriveConnect starts the OAuth flow and returns the consent URL to open.
export async function gdriveConnect(): Promise<{ consent_url: string }> {
  return postSync<{ consent_url: string }>("/1.0/knowledge/gdrive/connect");
}

// gdriveDisconnect deletes the stored Drive token.
export async function gdriveDisconnect(): Promise<void> {
  await postSync("/1.0/knowledge/gdrive/disconnect");
}

// gdriveResolve resolves a Drive folder or file URL into archives.
export async function gdriveResolve(url: string): Promise<GdriveResolveResult> {
  const r = await postSync<GdriveResolveResult>("/1.0/knowledge/gdrive/resolve", { url });
  return { kind: r.kind, archives: r.archives ?? [] };
}

// gdriveImport starts a tracked import of a single Drive archive. The UI issues
// one call per selected archive so each is an independently tracked operation.
export async function gdriveImport(
  archive: GdriveArchive,
  target: string,
  force: boolean
): Promise<OperationView> {
  const { metadata } = await postAsync<OperationView>("/1.0/knowledge/gdrive/import", {
    id: archive.id,
    name: archive.name,
    target: target || undefined,
    force,
  });
  return metadata;
}
