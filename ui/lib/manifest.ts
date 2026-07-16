import type { BatchManifest, BatchQuestion } from "./api/answer";

// ManifestParseError is thrown when a YAML manifest cannot be parsed or fails
// validation. Screens render its message as a field-level validation error and
// never send an invalid manifest to the API.
export class ManifestParseError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ManifestParseError";
  }
}

// parseManifest parses the flat `answer batch` YAML manifest subset into a
// BatchManifest and validates it. The manifest shape is intentionally simple
// (top-level scalars + a `questions:` list of `- question:` items, each with
// optional `id` and `keywords`), so a hand-rolled parser avoids adding a YAML
// dependency. Anything it cannot parse becomes a ManifestParseError.
export function parseManifest(text: string): BatchManifest {
  const lines = text.split(/\r?\n/);
  const manifest: BatchManifest = { questions: [] };
  let inQuestions = false;
  // When a top-level `knowledge_bases:` has no inline value, its items follow as
  // an indented block list ("  - name"); collect them here.
  let inKBBlock = false;
  let current: BatchQuestion | null = null;

  const flush = () => {
    if (current) {
      if (!current.question || !current.question.trim()) {
        throw new ManifestParseError("every question needs a non-empty `question` field");
      }
      manifest.questions.push(current);
      current = null;
    }
  };

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const line = stripComment(raw);
    if (!line.trim()) continue;

    const indent = raw.length - raw.trimStart().length;

    // Top-level keys (no indentation).
    if (indent === 0) {
      inQuestions = false;
      inKBBlock = false;
      flush();
      const { key, value } = splitKeyValue(raw, i);
      switch (key) {
        case "version":
          manifest.version = unquote(value);
          break;
        case "model":
          manifest.model = unquote(value);
          break;
        case "prompt":
          manifest.prompt = unquote(value);
          break;
        case "temperature": {
          const n = Number(unquote(value));
          if (Number.isNaN(n)) throw new ManifestParseError(`temperature must be a number (line ${i + 1})`);
          manifest.temperature = n;
          break;
        }
        case "knowledge_bases":
          if (value.trim()) {
            // Inline flow sequence: knowledge_bases: [a, b].
            manifest.knowledge_bases = parseInlineList(value);
          } else {
            // Block list: items follow on indented "- name" lines.
            manifest.knowledge_bases = [];
            inKBBlock = true;
          }
          break;
        case "questions":
          inQuestions = true;
          break;
        default:
          // Ignore unknown top-level keys for forward-compatibility.
          break;
      }
      continue;
    }

    const trimmed = line.trim();

    // Indented items of a knowledge_bases block list.
    if (inKBBlock) {
      if (trimmed.startsWith("-")) {
        manifest.knowledge_bases!.push(unquote(trimmed.slice(1).trim()));
        continue;
      }
      inKBBlock = false;
    }

    if (!inQuestions) continue;

    // A new list item under `questions:` begins with "- ".
    if (trimmed.startsWith("-")) {
      flush();
      current = { question: "" };
      const rest = trimmed.slice(1).trim();
      if (rest) {
        const { key, value } = splitKeyValue(rest, i);
        applyQuestionField(current, key, value, i);
      }
      continue;
    }

    // A continuation field of the current question item.
    if (current) {
      const { key, value } = splitKeyValue(trimmed, i);
      applyQuestionField(current, key, value, i);
    }
  }
  flush();

  if (manifest.questions.length === 0) {
    throw new ManifestParseError("manifest has no questions");
  }
  return manifest;
}

// applyQuestionField sets a recognized question field. Unknown fields are
// ignored (not rejected), matching the CLI's lenient yaml.Unmarshal — a
// manifest written by `answer batch --build` carries a `source` field per
// question that the CLI reader itself ignores, and we must accept the full
// schema the CLI emits rather than a narrower subset. `lineNo` is retained for
// symmetry with the other parse helpers' error reporting.
function applyQuestionField(q: BatchQuestion, key: string, value: string, _lineNo: number): void {
  switch (key) {
    case "id":
      q.id = unquote(value);
      break;
    case "question":
      q.question = unquote(value);
      break;
    case "keywords":
      q.keywords = parseInlineList(value);
      break;
    case "source":
      q.source = unquote(value);
      break;
    default:
      // Ignore unknown fields, as the CLI does (yaml.Unmarshal without
      // KnownFields), so forward-compatible manifests still load.
      break;
  }
}

function splitKeyValue(line: string, lineNo: number): { key: string; value: string } {
  const idx = line.indexOf(":");
  if (idx === -1) {
    throw new ManifestParseError(`expected "key: value" (line ${lineNo + 1})`);
  }
  return { key: line.slice(0, idx).trim(), value: line.slice(idx + 1).trim() };
}

// parseInlineList handles the inline `[a, b]` flow-sequence form used for
// knowledge_bases and keywords in the CLI's manifests. An empty value yields [].
function parseInlineList(value: string): string[] {
  const v = value.trim();
  if (!v) return [];
  const inner = v.startsWith("[") && v.endsWith("]") ? v.slice(1, -1) : v;
  return inner
    .split(",")
    .map((s) => unquote(s.trim()))
    .filter(Boolean);
}

function unquote(value: string): string {
  const v = value.trim();
  if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
    return v.slice(1, -1);
  }
  return v;
}

function stripComment(line: string): string {
  // Strip a trailing "# comment" that is not inside quotes. The manifest subset
  // never puts "#" inside a value, so a simple split is safe enough.
  const hashIdx = line.indexOf(" #");
  return hashIdx === -1 ? line : line.slice(0, hashIdx);
}

// serializeManifest renders a BatchManifest as YAML the CLI's `answer batch`
// accepts (rfp.Manifest / chat.BatchManifest shape). Questions carry id +
// question; knowledge_bases is emitted as a block list.
export function serializeManifest(manifest: BatchManifest): string {
  const out: string[] = [];
  out.push(`version: "${manifest.version ?? "1.0"}"`);
  if (manifest.model) out.push(`model: ${yamlScalar(manifest.model)}`);
  if (manifest.temperature !== undefined) out.push(`temperature: ${manifest.temperature}`);
  if (manifest.knowledge_bases && manifest.knowledge_bases.length > 0) {
    out.push("knowledge_bases:");
    for (const kb of manifest.knowledge_bases) out.push(`  - ${yamlScalar(kb)}`);
  }
  if (manifest.prompt) out.push(`prompt: ${yamlScalar(manifest.prompt)}`);
  out.push("questions:");
  manifest.questions.forEach((q, i) => {
    out.push(`  - id: ${yamlScalar(q.id ?? String(i + 1))}`);
    out.push(`    question: ${yamlScalar(q.question)}`);
    if (q.keywords && q.keywords.length > 0) {
      out.push(`    keywords: [${q.keywords.map(yamlScalar).join(", ")}]`);
    }
    // Preserve source (e.g. XLSX sheet name) when present so a CLI-generated
    // manifest round-trips unchanged. The CLI reader ignores it.
    if (q.source) {
      out.push(`    source: ${yamlScalar(q.source)}`);
    }
  });
  return out.join("\n") + "\n";
}

// yamlScalar quotes a scalar when needed (contains a colon, quote, leading
// special char, or is empty), escaping embedded double quotes.
function yamlScalar(value: string): string {
  if (value === "") return '""';
  if (/[:#"'\n]|^[\s>|&*!?%@`-]|:\s/.test(value)) {
    return `"${value.replace(/"/g, '\\"')}"`;
  }
  return value;
}
