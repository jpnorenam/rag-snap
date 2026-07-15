import {
  deleteSync,
  downloadFile,
  getSync,
  postAsync,
  postAsyncForm,
  postSync,
} from "./envelope";
import type { OperationView } from "./operations";

// KnowledgeBase is the API view of a knowledge base from GET /1.0/knowledge,
// matching the daemon's knowledgeBaseSummary.
export interface KnowledgeBase {
  name: string;
  index: string;
  health?: string;
  status?: string;
  docs_count?: string;
  store_size?: string;
  source_count: number;
}

// KnowledgeBaseDetail is the view from GET /1.0/knowledge/{name}.
export interface KnowledgeBaseDetail {
  name: string;
  index: string;
  chunk_count: number;
  source_count: number;
}

// SourceMetadata mirrors the daemon's stored source record. Fields beyond the
// core few are optional because older/partial records may omit them.
export interface SourceMetadata {
  source_id: string;
  file_name?: string;
  file_path?: string;
  content_type?: string;
  title?: string;
  author?: string;
  language?: string;
  index_name?: string;
  chunk_count?: number;
  content_length?: number;
  status?: string;
  ingested_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

// EngineInitResult carries the model IDs reported by the engine-init operation.
export interface EngineInitResult {
  embedding_model_id?: string;
  rerank_model_id?: string;
}

// BatchItem is one entry of a batch ingest request (JSON), mirroring the
// daemon's ingestItem.
export interface BatchItem {
  source_id?: string;
  type: "url" | "github" | "gitea" | "file";
  url?: string;
  source?: string;
  branch?: string;
  path?: string;
  extensions?: string[];
}

// listKnowledge fetches the available knowledge bases, sorted alphabetically by
// name (case-insensitive). Returns an empty array when the knowledge backend is
// unconfigured or empty.
export async function listKnowledge(): Promise<KnowledgeBase[]> {
  const bases = await getSync<KnowledgeBase[] | null>("/1.0/knowledge");
  return (bases ?? []).sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
}

// getKnowledge fetches a single base's detail (chunk + source counts).
export async function getKnowledge(name: string): Promise<KnowledgeBaseDetail> {
  return getSync<KnowledgeBaseDetail>(`/1.0/knowledge/${encodeURIComponent(name)}`);
}

// createKnowledge creates a base (sync). Rejects with ApiError on conflict.
export async function createKnowledge(name: string): Promise<KnowledgeBase> {
  return postSync<KnowledgeBase>("/1.0/knowledge", { name });
}

// deleteKnowledge removes a base and its source metadata (sync).
export async function deleteKnowledge(name: string): Promise<void> {
  await deleteSync(`/1.0/knowledge/${encodeURIComponent(name)}`);
}

// listSources returns a base's ingested sources ([] when none).
export async function listSources(name: string): Promise<SourceMetadata[]> {
  const sources = await getSync<SourceMetadata[] | null>(
    `/1.0/knowledge/${encodeURIComponent(name)}/sources`
  );
  return sources ?? [];
}

// getSource returns one source's metadata.
export async function getSource(name: string, id: string): Promise<SourceMetadata> {
  return getSync<SourceMetadata>(
    `/1.0/knowledge/${encodeURIComponent(name)}/sources/${encodeURIComponent(id)}`
  );
}

// forgetSource removes a source's chunks and metadata (sync).
export async function forgetSource(name: string, id: string): Promise<void> {
  await deleteSync(
    `/1.0/knowledge/${encodeURIComponent(name)}/sources/${encodeURIComponent(id)}`
  );
}

// initEngine starts the knowledge-engine initialization operation, returning the
// operation view to track.
export async function initEngine(): Promise<OperationView> {
  const { metadata } = await postAsync<OperationView>("/1.0/knowledge-engine");
  return metadata;
}

// ingestFile uploads a document to a base and returns the operation to track.
export async function ingestFile(
  name: string,
  file: File,
  sourceId: string,
  force: boolean
): Promise<OperationView> {
  const form = new FormData();
  form.append("file", file);
  if (sourceId) form.append("source_id", sourceId);
  if (force) form.append("force", "true");
  const { metadata } = await postAsyncForm<OperationView>(
    `/1.0/knowledge/${encodeURIComponent(name)}/sources`,
    form
  );
  return metadata;
}

// ingestUrl ingests a URL into a base and returns the operation to track.
export async function ingestUrl(
  name: string,
  url: string,
  sourceId: string,
  force: boolean
): Promise<OperationView> {
  const { metadata } = await postAsync<OperationView>(
    `/1.0/knowledge/${encodeURIComponent(name)}/sources`,
    { url, source_id: sourceId || undefined, force }
  );
  return metadata;
}

// ingestBatch runs a batch of items as a single operation.
export async function ingestBatch(
  name: string,
  batch: BatchItem[],
  force: boolean
): Promise<OperationView> {
  const { metadata } = await postAsync<OperationView>(
    `/1.0/knowledge/${encodeURIComponent(name)}/sources`,
    { batch, force }
  );
  return metadata;
}

// exportKnowledge starts an export operation and returns the operation to track.
export async function exportKnowledge(name: string): Promise<OperationView> {
  const { metadata } = await postAsync<OperationView>(
    `/1.0/knowledge/${encodeURIComponent(name)}/export`,
    {}
  );
  return metadata;
}

// downloadExportArchive fetches a completed export's archive to the browser.
// opId is the bare operation id (not the /1.0/operations/ path).
export async function downloadExportArchive(
  name: string,
  opId: string,
  filename: string
): Promise<void> {
  await downloadFile(
    `/1.0/knowledge/${encodeURIComponent(name)}/export/${encodeURIComponent(opId)}/archive`,
    filename
  );
}

// importKnowledge uploads an archive and starts an import operation.
export async function importKnowledge(
  archive: File,
  targetName: string,
  force: boolean
): Promise<OperationView> {
  const form = new FormData();
  form.append("archive", archive);
  if (targetName) form.append("name", targetName);
  if (force) form.append("force", "true");
  const { metadata } = await postAsyncForm<OperationView>("/1.0/knowledge/import", form);
  return metadata;
}
