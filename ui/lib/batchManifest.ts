import type { BatchItem } from "@/lib/api/knowledge";
import { yamlScalar } from "@/lib/manifest";

// PreviewEntry is a parsed manifest job for display + submission. `unsupported`
// marks entries the API cannot run (local `file` paths across the daemon
// boundary), which are shown but excluded from the batch.
export interface PreviewEntry {
  id: string;
  type: BatchItem["type"];
  source: string;
  unsupported?: string;
}

// BuilderJob is the builder's editable view of one manifest job, keyed by the
// CLI manifest job type names (`k ingest --batch` schema). Empty optionals are
// omitted on serialization.
export interface BuilderJob {
  name: string;
  type: "url" | "github-repo" | "gitea-repo" | "file";
  source: string;
  branch: string;
  path: string;
  extensions: string[];
  label: string;
}

// FILE_JOB_UNSUPPORTED explains why local `file` jobs cannot run over the API.
export const FILE_JOB_UNSUPPORTED =
  "Local file paths can’t be read by the daemon — upload the file instead.";

// JOB_TYPE_MAP maps the CLI manifest job types to API batch item types.
const JOB_TYPE_MAP: Record<string, BatchItem["type"]> = {
  url: "url",
  "github-repo": "github",
  "gitea-repo": "gitea",
  file: "file",
};

interface RawJob {
  name?: string;
  type?: string;
  source?: string;
  branch?: string;
  path?: string;
  extensions?: string[];
  label?: string;
}

// stripQuotes removes surrounding single/double quotes from a scalar value. A
// double-quoted value is unescaped, because that is what yamlScalar (shared with
// lib/manifest.ts) writes: without this, a value carrying a quote or a backslash
// reads back with its escapes intact and a serialize → parse round trip loses it.
function stripQuotes(v: string): string {
  const t = v.trim();
  if (t.startsWith('"') && t.endsWith('"') && t.length >= 2) {
    return t.slice(1, -1).replace(/\\(["\\nrt])/g, (_, c: string) => {
      if (c === "n") return "\n";
      if (c === "r") return "\r";
      if (c === "t") return "\t";
      return c;
    });
  }
  if (t.startsWith("'") && t.endsWith("'") && t.length >= 2) {
    // Single-quoted YAML has no escapes except a doubled quote.
    return t.slice(1, -1).replace(/''/g, "'");
  }
  return t;
}

// stripComment removes a trailing `# comment`, honouring YAML's rules enough
// for this schema: `#` starts a comment only outside quotes and only at line
// start or after whitespace (so quoted URLs with fragments survive).
function stripComment(line: string): string {
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (c === '"' && !inSingle) inDouble = !inDouble;
    else if (c === "'" && !inDouble) inSingle = !inSingle;
    else if (c === "#" && !inSingle && !inDouble && (i === 0 || /\s/.test(line[i - 1]))) {
      return line.slice(0, i);
    }
  }
  return line;
}

// parseInlineList parses a `[a, b, c]` inline YAML sequence.
function parseInlineList(v: string): string[] {
  const inner = v.trim().replace(/^\[/, "").replace(/\]$/, "");
  if (!inner.trim()) return [];
  return inner.split(",").map((x) => stripQuotes(x)).filter(Boolean);
}

// builderJobToBatchItem converts one builder job to the API batch item that
// runs it, or null when the API cannot run it (local `file` jobs, unknown
// types) — those are preserved for export but excluded from a run.
export function builderJobToBatchItem(job: BuilderJob): BatchItem | null {
  const mapped = JOB_TYPE_MAP[job.type];
  if (!mapped || mapped === "file") return null;
  if (mapped === "url") {
    return {
      type: "url",
      url: job.source,
      source_id: job.name || undefined,
      label: job.label || undefined,
    };
  }
  return {
    type: mapped,
    source: job.source,
    source_id: job.name || undefined,
    branch: job.branch || undefined,
    path: job.path || undefined,
    extensions: job.extensions.length > 0 ? job.extensions : undefined,
    label: job.label || undefined,
  };
}

// parseBatchManifest reads the documented batch YAML (version + jobs[]) with a
// purpose-built reader for that flat schema — the UI adds no YAML dependency.
// It returns the API batch items, a preview list, and the builder's editable
// jobs; malformed input yields an error string. It is not a general YAML parser.
export function parseBatchManifest(text: string): {
  items: BatchItem[];
  preview: PreviewEntry[];
  jobs: BuilderJob[];
  error?: string;
} {
  const lines = text.split(/\r?\n/);
  const rawJobs: RawJob[] = [];
  let current: RawJob | null = null;
  let inJobs = false;

  const assign = (job: RawJob, key: string, value: string) => {
    switch (key) {
      case "name":
        job.name = stripQuotes(value);
        break;
      case "type":
        job.type = stripQuotes(value);
        break;
      case "source":
        job.source = stripQuotes(value);
        break;
      case "branch":
        job.branch = stripQuotes(value);
        break;
      case "path":
        job.path = stripQuotes(value);
        break;
      case "extensions":
        job.extensions = parseInlineList(value);
        break;
      case "label":
        job.label = stripQuotes(value);
        break;
      default:
        break;
    }
  };

  for (const raw of lines) {
    const line = stripComment(raw);
    if (!line.trim()) continue;

    if (/^jobs:\s*$/.test(line.trim())) {
      inJobs = true;
      continue;
    }
    if (!inJobs) continue; // skip version and any preamble

    const itemMatch = line.match(/^\s*-\s*(.*)$/);
    if (itemMatch) {
      current = {};
      rawJobs.push(current);
      const rest = itemMatch[1];
      const kv = rest.match(/^([a-z_]+):\s*(.*)$/i);
      if (kv) assign(current, kv[1], kv[2]);
      continue;
    }
    const kv = line.match(/^\s+([a-z_]+):\s*(.*)$/i);
    if (kv && current) assign(current, kv[1], kv[2]);
  }

  if (rawJobs.length === 0) {
    return { items: [], preview: [], jobs: [], error: "No jobs found. Expected a `jobs:` list." };
  }

  const items: BatchItem[] = [];
  const preview: PreviewEntry[] = [];
  const jobs: BuilderJob[] = [];
  for (const job of rawJobs) {
    const mapped = job.type ? JOB_TYPE_MAP[job.type] : undefined;
    const source = job.source ?? "";
    const id = job.name || source;
    jobs.push({
      name: job.name ?? "",
      type: (mapped ? job.type : "file") as BuilderJob["type"],
      source,
      branch: job.branch ?? "",
      path: job.path ?? "",
      extensions: job.extensions ?? [],
      label: job.label ?? "",
    });
    if (!mapped) {
      preview.push({ id, type: "file", source, unsupported: `Unknown job type “${job.type ?? ""}”.` });
      continue;
    }
    if (mapped === "file") {
      preview.push({ id, type: "file", source, unsupported: FILE_JOB_UNSUPPORTED });
      continue;
    }
    preview.push({ id, type: mapped, source });
    if (mapped === "url") {
      items.push({ type: "url", url: source, source_id: job.name, label: job.label });
    } else {
      items.push({
        type: mapped,
        source,
        source_id: job.name,
        branch: job.branch,
        path: job.path,
        extensions: job.extensions,
        label: job.label,
      });
    }
  }

  return { items, preview, jobs };
}

// serializeBatchManifest renders builder jobs as the YAML `k ingest --batch`
// accepts — the inverse of parseBatchManifest, so parse(serialize(jobs))
// round-trips. Empty optional fields are omitted.
export function serializeBatchManifest(jobs: BuilderJob[]): string {
  const out: string[] = ['version: "1"', "jobs:"];
  for (const job of jobs) {
    const fields: string[] = [];
    if (job.name) fields.push(`name: ${yamlScalar(job.name)}`);
    fields.push(`type: ${yamlScalar(job.type)}`);
    fields.push(`source: ${yamlScalar(job.source)}`);
    if (job.branch) fields.push(`branch: ${yamlScalar(job.branch)}`);
    if (job.path) fields.push(`path: ${yamlScalar(job.path)}`);
    if (job.extensions.length > 0) {
      fields.push(`extensions: [${job.extensions.map(yamlScalar).join(", ")}]`);
    }
    if (job.label) fields.push(`label: ${yamlScalar(job.label)}`);
    out.push(`  - ${fields[0]}`);
    for (const field of fields.slice(1)) out.push(`    ${field}`);
  }
  return out.join("\n") + "\n";
}
