import { postAsync, postAsyncForm, postSync } from "./envelope";
import type { OperationView } from "./operations";

// BatchQuestion is a single question in a prepared manifest. `id`, `question`,
// and `keywords` match the daemon's batchQuestionRequest (POST /1.0/answer/batch).
// `source` is written by the CLI's `answer batch --build` (rfp.Question, e.g. an
// XLSX sheet name) and is read by the run: it is the fallback `domains` match key
// for a question whose `id` is a bare sequence number, so it must be sent, not
// merely retained for a round trip.
export interface BatchQuestion {
  id?: string;
  question: string;
  keywords?: string[];
  source?: string;
}

// BatchDomain is one entry of a manifest's `domains` routing table, matching the
// daemon's batchDomainRequest. `match` is a glob over `*` and `?` compared
// case-insensitively against a question's `id` (or its `source`, for a bare
// numeric id); `context` is the requirement domain injected into that question's
// turn, and `keywords` are retrieval terms it inherits. An entry needs at least
// one of `context` and `keywords`.
export interface BatchDomain {
  match: string;
  context?: string;
  keywords?: string[];
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
  domains?: BatchDomain[];
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
  const { operation, metadata } = await postAsyncForm<OperationView>("/1.0/answer/build", form);
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

// --- RFP analysis ---------------------------------------------------------
//
// Analysis is the alternative to extraction: it hands the documents to a
// document-capable model and returns a complete manifest — questions, knowledge
// bases, an RFP-level prompt and a populated domains block. It runs in two passes
// for the same reason the build flow does: the interview between them is
// conditioned on what the first pass found, so the OpenStack question is asked
// only when the RFP actually requires OpenStack.

// ANALYSIS_EXTENSION is the capability the daemon advertises only when analysis can
// actually run — it needs analysis enabled, a Gemini model id and a GEMINI_API_KEY
// that a given installation may not have. The UI reads it to present the path as
// unavailable rather than offering it and failing at submit time.
export const ANALYSIS_EXTENSION = "answer_analyze";

// TriageSection is one section, sheet or part the triage found. `id_prefix` is the
// question-id prefix its rows use, which is what a domains pattern keys on.
export interface TriageSection {
  name: string;
  id_prefix?: string;
  estimated_questions?: number;
  summary?: string;
}

// ConveyanceReport says how one document reached the model. `path` is "native"
// (the model read the original bytes) or "converted" (rag-cli turned it into
// table-preserving text first, because no model API accepts a spreadsheet). A
// converted document changes how much to trust the result, so it is surfaced.
export interface ConveyanceReport {
  filename: string;
  format: string;
  path: "native" | "converted";
  converted: boolean;
  detail?: string;
  bytes: number;
}

// Triage is the first pass: what the documents are, before any manifest exists.
export interface Triage {
  title?: string;
  issuer?: string;
  sections: TriageSection[];
  estimated_questions: number;
  // openstack_in_scope gates the OpenStack interview question.
  openstack_in_scope: boolean;
  openstack_evidence?: string;
  // multi_field_answers says each question expects more than one field filled.
  multi_field_answers: boolean;
  multi_field_detail?: string;
  suggested_knowledge_bases: string[];
  suggested_kapa_source_groups: string[];
  notes?: string;
  report: ConveyanceReport[];
  model: string;
}

// AnalyzeMetadata is published on the triage operation once it completes.
export interface AnalyzeMetadata {
  analysisToken: string;
  expiresAt: string;
  model: string;
  triage: Triage;
}

// CheckSeverity distinguishes a manifest that will not run, or names a retrieval
// target that contributes nothing, from one that runs but may not be intended.
export type CheckSeverity = "defect" | "warning";

export interface CheckFinding {
  severity: CheckSeverity;
  code: string;
  message: string;
  subject?: string;
}

export interface DomainMatchCount {
  match: string;
  count: number;
}

// PromptAssertions records what a manifest's inline prompt still carries.
// `applicable` is false for a manifest with no inline prompt.
export interface PromptAssertions {
  applicable: boolean;
  no_context_rule: boolean;
  directness_rule: boolean;
  length_stated: boolean;
  length_hard: boolean;
}

// CheckResult is the outcome of checking one manifest. `references_checked` and
// `kapa_checked` are reported separately, and each is false when that check did
// not run — silence about a check that never happened would be false reassurance.
export interface CheckResult {
  questions: number;
  domains: number;
  domain_matches: DomainMatchCount[];
  findings: CheckFinding[];
  references_checked: boolean;
  kapa_checked: boolean;
  prompt: PromptAssertions;
}

// GenerateMetadata is published on the generation operation once it completes.
export interface GenerateMetadata {
  manifest: BatchManifest;
  check: CheckResult;
  model: string;
}

// AnswerLengthMode says whether the target answer length may be exceeded.
export type AnswerLengthMode = "hard" | "soft";

// OpenStackFlavour is the offering answers should present. Only meaningful when
// the triage reported OpenStack in scope.
export type OpenStackFlavour = "sunbeam" | "charmed";

// InterviewAnswers are the operator's answers, which materially shape the manifest.
export interface InterviewAnswers {
  analysisToken: string;
  answerWords: number;
  answerLimit: AnswerLengthMode;
  openstack?: OpenStackFlavour;
}

// analyzeDocuments uploads the primary RFP plus any number of supporting context
// documents to POST /1.0/answer/analyze and returns the async triage operation.
// The triage, the per-document conveyance report and the session token are
// published on the operation metadata once it completes.
export async function analyzeDocuments(
  file: File,
  contextFiles: File[] = []
): Promise<TrackedOperation> {
  const form = new FormData();
  form.append("file", file);
  for (const f of contextFiles) form.append("context", f);
  const { operation, metadata } = await postAsyncForm<OperationView>("/1.0/answer/analyze", form);
  return { operation, view: metadata };
}

// generateManifest runs the second pass against a triage token plus the interview
// answers. The documents are not re-uploaded.
export async function generateManifest(answers: InterviewAnswers): Promise<TrackedOperation> {
  const body: Record<string, unknown> = {
    analysis_token: answers.analysisToken,
    answer_words: answers.answerWords,
    answer_limit: answers.answerLimit,
  };
  if (answers.openstack) body.openstack = answers.openstack;
  const { operation, metadata } = await postAsync<OperationView>(
    "/1.0/answer/analyze/generate",
    body
  );
  return { operation, view: metadata };
}

// checkManifest validates a manifest without running it. Synchronous: no model
// call, no run, nothing stored.
export function checkManifest(manifest: BatchManifest): Promise<CheckResult> {
  return postSync<CheckResult>("/1.0/answer/check", manifest);
}

// parseAnalyzeMetadata reads the triage operation's metadata, normalising the null
// arrays the daemon may send to empty ones so callers can map over them freely.
export function parseAnalyzeMetadata(meta: Record<string, unknown>): AnalyzeMetadata {
  const token = typeof meta.analysis_token === "string" ? meta.analysis_token : "";
  if (!token) {
    throw new BuildMetadataError("the analysis response carried no session token");
  }
  const raw = (meta.triage ?? {}) as Partial<Triage>;
  return {
    analysisToken: token,
    expiresAt: typeof meta.expires_at === "string" ? meta.expires_at : "",
    model: typeof meta.model === "string" ? meta.model : "",
    triage: {
      title: raw.title,
      issuer: raw.issuer,
      sections: raw.sections ?? [],
      estimated_questions: raw.estimated_questions ?? 0,
      openstack_in_scope: raw.openstack_in_scope === true,
      openstack_evidence: raw.openstack_evidence,
      multi_field_answers: raw.multi_field_answers === true,
      multi_field_detail: raw.multi_field_detail,
      suggested_knowledge_bases: raw.suggested_knowledge_bases ?? [],
      suggested_kapa_source_groups: raw.suggested_kapa_source_groups ?? [],
      notes: raw.notes,
      report: raw.report ?? [],
      model: raw.model ?? "",
    },
  };
}

// parseGenerateMetadata reads the generation operation's metadata.
export function parseGenerateMetadata(meta: Record<string, unknown>): GenerateMetadata {
  const manifest = meta.manifest as BatchManifest | undefined;
  if (!manifest || !Array.isArray(manifest.questions)) {
    throw new BuildMetadataError("the generation response carried no manifest");
  }
  return {
    manifest: {
      ...manifest,
      questions: manifest.questions ?? [],
      domains: manifest.domains ?? [],
      knowledge_bases: manifest.knowledge_bases ?? [],
    },
    check: normaliseCheck(meta.check as Partial<CheckResult> | undefined),
    model: typeof meta.model === "string" ? meta.model : "",
  };
}

// normaliseCheck fills in the arrays the daemon omits when empty.
export function normaliseCheck(raw: Partial<CheckResult> | undefined): CheckResult {
  return {
    questions: raw?.questions ?? 0,
    domains: raw?.domains ?? 0,
    domain_matches: raw?.domain_matches ?? [],
    findings: raw?.findings ?? [],
    references_checked: raw?.references_checked === true,
    kapa_checked: raw?.kapa_checked === true,
    prompt: raw?.prompt ?? {
      applicable: false,
      no_context_rule: false,
      directness_rule: false,
      length_stated: false,
      length_hard: false,
    },
  };
}

// defects and warnings split findings by severity. A manifest carrying only
// warnings must be distinguishable from both a clean and a defective one, since
// the common case — a domain pattern reserved for a section with no questions yet
// — is warnings-only and must not read as a failure.
export function defects(result: CheckResult): CheckFinding[] {
  return result.findings.filter((f) => f.severity === "defect");
}

export function warnings(result: CheckResult): CheckFinding[] {
  return result.findings.filter((f) => f.severity === "warning");
}
