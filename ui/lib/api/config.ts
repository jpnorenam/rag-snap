import { deleteSync, getSync, putSync } from "./envelope";

// REDACTED is the marker the daemon substitutes for a secret-shaped value. The
// secret itself never reaches the browser; the key stays writable (write-only).
export const REDACTED = "<redacted>";

// ConfigLayer is where an effective value comes from. A "user" value overrides
// the "package" value shipped by the snap, and only a "user" value can be reverted.
export type ConfigLayer = "package" | "user";

// ConfigEntry is one configuration key: its effective value and the layer that
// value resolves from.
export interface ConfigEntry {
  key: string;
  value: string;
  layer: ConfigLayer;
  // The value a revert would restore. Present only on a "user" entry — it is what
  // the override is hiding, which a revert confirmation has to show.
  package_value?: string;
}

// ConfigList is the metadata of GET /1.0/config. `writable` reports whether this
// caller may write, so the page can render read-only rather than show edit
// controls that are certain to fail.
export interface ConfigList {
  writable: boolean;
  keys: ConfigEntry[];
}

// isRedacted reports whether an entry's value is a redaction marker rather than
// the real value.
export function isRedacted(entry: ConfigEntry): boolean {
  return entry.value === REDACTED;
}

// getConfig fetches the effective configuration, sorted by key, with deprecated
// keys already hidden by the daemon.
export async function getConfig(): Promise<ConfigList> {
  const list = await getSync<ConfigList>("/1.0/config");
  return { writable: list.writable, keys: list.keys ?? [] };
}

// setConfig writes a value to the user layer — the same override the CLI's
// `rag-cli.rag set <key>=<value>` writes. The daemon rejects unknown keys: the
// API can override existing keys, never create new ones.
export function setConfig(key: string, value: string): Promise<ConfigEntry> {
  return putSync<ConfigEntry>(`/1.0/config/${key}`, { value });
}

// revertConfig drops the user-layer override so the package value applies again.
export function revertConfig(key: string): Promise<ConfigEntry> {
  return deleteSync<ConfigEntry>(`/1.0/config/${key}`);
}
