import type { BatchItem } from "@/lib/api/knowledge";

// PreviewEntry is a parsed manifest job for display + submission. `unsupported`
// marks entries the API cannot run (local `file` paths across the daemon
// boundary), which are shown but excluded from the batch.
export interface PreviewEntry {
  id: string;
  type: BatchItem["type"];
  source: string;
  unsupported?: string;
}

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
}

// stripQuotes removes surrounding single/double quotes from a scalar value.
function stripQuotes(v: string): string {
  const t = v.trim();
  if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'"))) {
    return t.slice(1, -1);
  }
  return t;
}

// parseInlineList parses a `[a, b, c]` inline YAML sequence.
function parseInlineList(v: string): string[] {
  const inner = v.trim().replace(/^\[/, "").replace(/\]$/, "");
  if (!inner.trim()) return [];
  return inner.split(",").map((x) => stripQuotes(x)).filter(Boolean);
}

// parseBatchManifest reads the documented batch YAML (version + jobs[]) with a
// purpose-built reader for that flat schema — the UI adds no YAML dependency.
// It returns the API batch items plus a preview list; malformed input yields an
// error string. It is not a general YAML parser.
export function parseBatchManifest(text: string): {
  items: BatchItem[];
  preview: PreviewEntry[];
  error?: string;
} {
  const lines = text.split(/\r?\n/);
  const jobs: RawJob[] = [];
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
      default:
        break;
    }
  };

  for (const raw of lines) {
    const line = raw.replace(/#.*$/, "");
    if (!line.trim()) continue;

    if (/^jobs:\s*$/.test(line.trim())) {
      inJobs = true;
      continue;
    }
    if (!inJobs) continue; // skip version and any preamble

    const itemMatch = line.match(/^\s*-\s*(.*)$/);
    if (itemMatch) {
      current = {};
      jobs.push(current);
      const rest = itemMatch[1];
      const kv = rest.match(/^([a-z_]+):\s*(.*)$/i);
      if (kv) assign(current, kv[1], kv[2]);
      continue;
    }
    const kv = line.match(/^\s+([a-z_]+):\s*(.*)$/i);
    if (kv && current) assign(current, kv[1], kv[2]);
  }

  if (jobs.length === 0) {
    return { items: [], preview: [], error: "No jobs found. Expected a `jobs:` list." };
  }

  const items: BatchItem[] = [];
  const preview: PreviewEntry[] = [];
  for (const job of jobs) {
    const mapped = job.type ? JOB_TYPE_MAP[job.type] : undefined;
    const source = job.source ?? "";
    const id = job.name || source;
    if (!mapped) {
      preview.push({ id, type: "file", source, unsupported: `Unknown job type “${job.type ?? ""}”.` });
      continue;
    }
    if (mapped === "file") {
      preview.push({
        id,
        type: "file",
        source,
        unsupported: "Local file paths can’t be read by the daemon — upload the file instead.",
      });
      continue;
    }
    preview.push({ id, type: mapped, source });
    if (mapped === "url") {
      items.push({ type: "url", url: source, source_id: job.name });
    } else {
      items.push({
        type: mapped,
        source,
        source_id: job.name,
        branch: job.branch,
        path: job.path,
        extensions: job.extensions,
      });
    }
  }

  return { items, preview };
}
