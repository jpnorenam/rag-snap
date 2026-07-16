import { postAsync } from "./envelope";
import type { OperationView } from "./operations";

// BatchQuestion is a single question in a prepared manifest. `id`, `question`,
// and `keywords` match the daemon's batchQuestionRequest (POST /1.0/answer/batch).
// `source` is written by the CLI's `answer batch --build` (rfp.Question, e.g. an
// XLSX sheet name); the daemon and the CLI reader both ignore it, but we retain
// it so a manifest round-trips unchanged.
export interface BatchQuestion {
  id?: string;
  question: string;
  keywords?: string[];
  source?: string;
}

// BatchManifest is the prepared manifest body accepted by POST /1.0/answer/batch,
// matching the daemon's batchManifestRequest. Temperature defaults to 0.1
// server-side when omitted.
export interface BatchManifest {
  version?: string;
  model?: string;
  knowledge_bases?: string[];
  prompt?: string;
  temperature?: number;
  questions: BatchQuestion[];
}

// BuildQuestion is a candidate question extracted by the build flow, matching
// the daemon's buildQuestionJSON (rfp.Question shape).
export interface BuildQuestion {
  id: string;
  question: string;
  source?: string;
}

// BuildColumn describes one column of a parsed table in the inspect response.
export interface BuildColumn {
  index: number;
  sample: string[];
  avg_len: number;
  suggested: boolean;
}

// BuildTable is one parsed table (a spreadsheet sheet, or the single CSV table).
export interface BuildTable {
  name: string;
  page_index: number;
  header: string[];
  row_count: number;
  columns: BuildColumn[];
}

// BuildInspectMetadata is the completion metadata for a tabular (XLSX/CSV)
// build: the daemon parsed the document and needs the client to choose a column
// before extracting. Discriminated from the extract shape by `needs_column`.
export interface BuildInspectMetadata {
  needs_column: true;
  build_token: string;
  format: "xlsx" | "csv";
  tables: BuildTable[];
  suggested: { table_index: number; column_index: number };
}

// BuildExtractMetadata is the completion metadata for a free-text build and for
// build/extract: the extracted candidate questions.
export interface BuildExtractMetadata {
  needs_column?: false;
  questions: BuildQuestion[];
  count?: number;
  refined?: boolean;
}

// BuildMetadata is the discriminated completion metadata of POST /1.0/answer/build.
export type BuildMetadata = BuildInspectMetadata | BuildExtractMetadata;

// needsColumn narrows build metadata to the inspect (column-choice) case.
export function needsColumn(meta: Record<string, unknown>): boolean {
  return meta?.needs_column === true;
}

// BuildMetadataError is thrown when tabular build metadata does not match the
// expected shape (e.g. a daemon/version mismatch, or a corrupted response).
// Callers surface its message instead of letting a bad shape crash the render.
export class BuildMetadataError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "BuildMetadataError";
  }
}

// parseInspectMetadata validates and normalizes the pass-1 (inspect) metadata
// of a spreadsheet/CSV build into a BuildInspectMetadata whose arrays are always
// present (never null/undefined), so the column step can index them safely.
// Throws BuildMetadataError on anything it cannot make sense of.
export function parseInspectMetadata(meta: Record<string, unknown>): BuildInspectMetadata {
  const token = meta.build_token;
  if (typeof token !== "string" || token === "") {
    throw new BuildMetadataError("build response is missing its build token");
  }
  const rawTables = meta.tables;
  if (!Array.isArray(rawTables) || rawTables.length === 0) {
    throw new BuildMetadataError("build response contained no tables to choose from");
  }

  const tables: BuildTable[] = rawTables.map((t, ti) => {
    const rt = (t ?? {}) as Record<string, unknown>;
    const rawCols = Array.isArray(rt.columns) ? rt.columns : [];
    const columns: BuildColumn[] = rawCols.map((c, ci) => {
      const rc = (c ?? {}) as Record<string, unknown>;
      return {
        index: typeof rc.index === "number" ? rc.index : ci,
        // The crux of the crash: samples may arrive as null/absent — normalize
        // to an array of strings so `.length`/`.join` are always safe.
        sample: Array.isArray(rc.sample) ? rc.sample.map((s) => String(s)) : [],
        avg_len: typeof rc.avg_len === "number" ? rc.avg_len : 0,
        suggested: rc.suggested === true,
      };
    });
    if (columns.length === 0) {
      throw new BuildMetadataError(`table ${ti + 1} has no columns`);
    }
    return {
      name: typeof rt.name === "string" ? rt.name : `Table ${ti + 1}`,
      page_index: typeof rt.page_index === "number" ? rt.page_index : 0,
      header: Array.isArray(rt.header) ? rt.header.map((h) => String(h)) : [],
      row_count: typeof rt.row_count === "number" ? rt.row_count : 0,
      columns,
    };
  });

  const rawSug = (meta.suggested ?? {}) as Record<string, unknown>;
  const tableIndex = typeof rawSug.table_index === "number" ? rawSug.table_index : 0;
  const columnIndex = typeof rawSug.column_index === "number" ? rawSug.column_index : 0;
  // Clamp the suggestion into range so a bad index can't select a missing table.
  const safeTable = tableIndex >= 0 && tableIndex < tables.length ? tableIndex : 0;
  const colCount = tables[safeTable].columns.length;
  const safeColumn = columnIndex >= 0 && columnIndex < colCount ? columnIndex : 0;

  return {
    needs_column: true,
    build_token: token,
    format: meta.format === "csv" ? "csv" : "xlsx",
    tables,
    suggested: { table_index: safeTable, column_index: safeColumn },
  };
}

// BuildExtractOptions mirror the POST /1.0/answer/build/extract request body.
export interface BuildExtractOptions {
  buildToken: string;
  tableIndex: number;
  columnIndex: number;
  idColumnIndex?: number;
  minLength?: number;
  refine?: boolean;
}

// TrackedOperation pairs the operation's canonical URL with its view. The
// caller registers the view with the operations tracker via track().
export interface TrackedOperation {
  operation: string;
  view: OperationView;
}

// runBatch posts a prepared manifest to POST /1.0/answer/batch and returns the
// async operation. The returned view is registered with the operations tracker
// by the caller; progress and results are read from its metadata.
export async function runBatch(manifest: BatchManifest): Promise<TrackedOperation> {
  const { operation, metadata } = await postAsync<OperationView>("/1.0/answer/batch", manifest);
  return { operation, view: metadata };
}

// buildFromDocument uploads a document to POST /1.0/answer/build and returns the
// async extraction operation. The extracted questions are published on the
// operation metadata's `questions` key once it completes.
export async function buildFromDocument(
  file: File,
  opts: { refine?: boolean } = {}
): Promise<TrackedOperation> {
  const form = new FormData();
  form.append("file", file);
  // Default is refine on; only send "false" to disable it.
  if (opts.refine === false) form.append("refine", "false");
  const { operation, metadata } = await postAsync<OperationView>("/1.0/answer/build", form);
  return { operation, view: metadata };
}

// buildExtract runs the second pass for a spreadsheet/CSV build: it extracts
// questions from a chosen column of the tables staged under `buildToken` by a
// prior buildFromDocument call. Returns the async operation; the questions are
// published on its metadata's `questions` key on completion.
export async function buildExtract(opts: BuildExtractOptions): Promise<TrackedOperation> {
  const body: Record<string, unknown> = {
    build_token: opts.buildToken,
    table_index: opts.tableIndex,
    column_index: opts.columnIndex,
  };
  if (opts.idColumnIndex !== undefined) body.id_column_index = opts.idColumnIndex;
  if (opts.minLength !== undefined) body.min_length = opts.minLength;
  if (opts.refine !== undefined) body.refine = opts.refine;
  const { operation, metadata } = await postAsync<OperationView>("/1.0/answer/build/extract", body);
  return { operation, view: metadata };
}
