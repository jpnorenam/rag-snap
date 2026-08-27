import { LineCounter, parseDocument, type Document } from "yaml";
import type { BatchDomain, BatchManifest, BatchQuestion } from "./api/answer";
import { compileDomains, DomainError, isBareSequence } from "./domains";

// ManifestParseError is thrown when a YAML manifest cannot be parsed or fails
// validation. Screens render its message as a field-level validation error and
// never send an invalid manifest to the API.
export class ManifestParseError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ManifestParseError";
  }
}

// parseManifest reads an `answer batch` YAML manifest into a BatchManifest and
// validates it. Reading is delegated to a real YAML parser: the hand-rolled
// line-based reader this replaces could not represent block scalars (a
// multi-line `prompt:` was silently truncated to its `|-` header) or nested
// block sequences, and the manifests it had to read are written by
// gopkg.in/yaml.v3, which uses both. Parsing and validation are separate: the
// document is parsed first, then validated field by field so every rejection is
// a ManifestParseError the screens can render.
export function parseManifest(text: string): BatchManifest {
  const lineCounter = new LineCounter();
  const doc = parseDocument(text, { lineCounter });
  if (doc.errors.length > 0) {
    // The parser's own messages already carry "at line N, column M".
    throw new ManifestParseError(`invalid YAML: ${doc.errors[0].message}`);
  }
  return validateManifest(doc.toJS(), (path) => nodeLine(doc, lineCounter, path));
}

// locate resolves a path in the parsed document to its 1-based source line, so a
// validation message can point at the offending field the way the previous
// reader's "(line N)" messages did.
type locate = (path: (string | number)[]) => number | undefined;

function nodeLine(doc: Document, lineCounter: LineCounter, path: (string | number)[]): number | undefined {
  const node = doc.getIn(path, true) as { range?: [number, number, number] } | undefined;
  const offset = node?.range?.[0];
  return offset === undefined ? undefined : lineCounter.linePos(offset).line;
}

function at(message: string, line: number | undefined): string {
  return line === undefined ? message : `${message} (line ${line})`;
}

// validateManifest is the typed layer over the parsed document: it accepts only
// the fields `answer batch` honours, coerces scalars the way the CLI's decoder
// does, and ignores unknown keys for forward compatibility (the CLI reads with
// yaml.Unmarshal without KnownFields, so a manifest carrying a field this UI
// does not know must still load).
function validateManifest(root: unknown, locate: locate): BatchManifest {
  // An empty document has no questions; report that rather than a shape error.
  if (root === null || root === undefined) {
    throw new ManifestParseError("manifest has no questions");
  }
  if (typeof root !== "object" || Array.isArray(root)) {
    throw new ManifestParseError("manifest must be a mapping of fields with a `questions:` list");
  }
  const raw = root as Record<string, unknown>;
  const manifest: BatchManifest = { questions: [] };

  const version = asString(raw.version);
  if (version !== undefined) manifest.version = version;
  const model = asString(raw.model);
  if (model !== undefined) manifest.model = model;
  const prompt = asString(raw.prompt);
  if (prompt !== undefined) manifest.prompt = prompt;

  if (raw.temperature !== undefined && raw.temperature !== null) {
    const n = typeof raw.temperature === "number" ? raw.temperature : Number(asString(raw.temperature));
    if (!Number.isFinite(n)) {
      throw new ManifestParseError(at("temperature must be a number", locate(["temperature"])));
    }
    manifest.temperature = n;
  }

  const bases = asStringList(raw.knowledge_bases);
  if (bases !== undefined) manifest.knowledge_bases = bases;

  const domains = validateDomains(raw.domains, locate);
  if (domains !== undefined) manifest.domains = domains;

  const rawQuestions = raw.questions;
  if (rawQuestions !== undefined && rawQuestions !== null && !Array.isArray(rawQuestions)) {
    throw new ManifestParseError(at("`questions` must be a list", locate(["questions"])));
  }
  const items: unknown[] = Array.isArray(rawQuestions) ? rawQuestions : [];
  manifest.questions = items.map((item, i) => validateQuestion(item, i, locate));

  if (manifest.questions.length === 0) {
    throw new ManifestParseError("manifest has no questions");
  }
  return manifest;
}

// validateDomains reads the optional `domains` routing table and puts it through
// the same rules the daemon applies before a run (compileDomains, which mirrors
// chat.CompileDomains). Rejecting here rather than on the server is the point of
// the client-side copy: an unfillable entry or a duplicated pattern becomes a
// field-level error on the upload screen instead of a 400 after the file is sent.
function validateDomains(value: unknown, locate: locate): BatchDomain[] | undefined {
  if (value === undefined || value === null) return undefined;
  if (!Array.isArray(value)) {
    throw new ManifestParseError(at("`domains` must be a list", locate(["domains"])));
  }
  if (value.length === 0) return undefined;

  const domains = value.map((item, i) => {
    if (item === null || typeof item !== "object" || Array.isArray(item)) {
      throw new ManifestParseError(
        at(`domains entry ${i + 1} must be a mapping with a \`match\` field`, locate(["domains", i]))
      );
    }
    const raw = item as Record<string, unknown>;
    const domain: BatchDomain = { match: asString(raw.match) ?? "" };
    const context = asString(raw.context);
    if (context !== undefined) domain.context = context;
    const keywords = asStringList(raw.keywords);
    if (keywords !== undefined) domain.keywords = keywords;
    return domain;
  });

  try {
    compileDomains(domains);
  } catch (e) {
    if (e instanceof DomainError) {
      throw new ManifestParseError(
        at(e.message, e.entry < 0 ? undefined : locate(["domains", e.entry]))
      );
    }
    throw e;
  }
  return domains;
}

function validateQuestion(item: unknown, index: number, locate: locate): BatchQuestion {
  if (item === null || typeof item !== "object" || Array.isArray(item)) {
    throw new ManifestParseError(
      at(`question ${index + 1} must be a mapping with a \`question\` field`, locate(["questions", index]))
    );
  }
  const raw = item as Record<string, unknown>;

  const question = asString(raw.question);
  if (question === undefined || !question.trim()) {
    throw new ManifestParseError("every question needs a non-empty `question` field");
  }
  const q: BatchQuestion = { question };

  // An unquoted numeric id ("- id: 4") parses as a number; the CLI's id is a
  // string, so coerce rather than reject.
  const id = asString(raw.id);
  if (id !== undefined) q.id = id;
  const keywords = asStringList(raw.keywords);
  if (keywords !== undefined) q.keywords = keywords;
  const source = asString(raw.source);
  if (source !== undefined) q.source = source;
  return q;
}

// asString coerces a scalar to a string, mirroring how the CLI's decoder fills a
// string field. A mapping or sequence where a scalar belongs yields undefined,
// so the field reads as absent and the caller's own rule (required or optional)
// decides the outcome.
function asString(value: unknown): string | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return undefined;
}

// asStringList reads a sequence of scalars in either YAML form — inline
// (`[a, b]`) or a block sequence — dropping empty entries. A bare scalar is
// accepted as a one-item list, which the previous reader also allowed.
function asStringList(value: unknown): string[] | undefined {
  if (value === undefined || value === null) return undefined;
  if (Array.isArray(value)) {
    return value
      .map((v) => asString(v))
      .filter((v): v is string => v !== undefined && v !== "");
  }
  const one = asString(value);
  return one === undefined || one === "" ? undefined : [one];
}

// SerializeOptions tune what serializeManifest writes around the manifest body.
export interface SerializeOptions {
  // domainsStub writes the commented-out `domains:` block above the manifest,
  // as the CLI's `answer batch --build` does (rfp.WriteManifest). Only the build
  // flow sets it: a manifest the user uploaded and downloaded again should come
  // back as it went in. Ignored when the manifest already carries routing —
  // there is nothing to suggest, and a stub above a live block reads as if the
  // two were alternatives.
  domainsStub?: boolean;
}

// serializeManifest renders a BatchManifest as YAML the CLI's `answer batch`
// accepts (rfp.Manifest / chat.BatchManifest shape). Questions carry id +
// question; knowledge_bases and domains are emitted as block lists. Everything
// parseManifest reads is written back, so an upload → preview → download round
// trip returns a manifest that routes the same way.
export function serializeManifest(manifest: BatchManifest, opts: SerializeOptions = {}): string {
  const out: string[] = [];
  out.push(`version: "${manifest.version ?? "1.0"}"`);
  if (manifest.model) emitScalarField(out, "", "model", manifest.model);
  if (manifest.temperature !== undefined) out.push(`temperature: ${manifest.temperature}`);
  if (manifest.knowledge_bases && manifest.knowledge_bases.length > 0) {
    out.push("knowledge_bases:");
    for (const kb of manifest.knowledge_bases) out.push(`  - ${yamlScalar(kb)}`);
  }
  if (manifest.prompt) emitScalarField(out, "", "prompt", manifest.prompt);
  // Domains are emitted in document order, which is what breaks a specificity
  // tie, and ahead of the questions as the CLI's encoder writes them.
  if (manifest.domains && manifest.domains.length > 0) {
    out.push("domains:");
    for (const d of manifest.domains) {
      out.push(`  - match: ${yamlScalar(d.match)}`);
      if (d.context) emitScalarField(out, "    ", "context", d.context);
      if (d.keywords && d.keywords.length > 0) {
        out.push(`    keywords: [${d.keywords.map(yamlScalar).join(", ")}]`);
      }
    }
  }
  out.push("questions:");
  manifest.questions.forEach((q, i) => {
    out.push(`  - id: ${yamlScalar(q.id ?? String(i + 1))}`);
    emitScalarField(out, "    ", "question", q.question);
    if (q.keywords && q.keywords.length > 0) {
      out.push(`    keywords: [${q.keywords.map(yamlScalar).join(", ")}]`);
    }
    // Preserve source (e.g. XLSX sheet name) when present so a CLI-generated
    // manifest round-trips unchanged. `answer batch` reads it as the fallback
    // domain match key for a question whose id is a bare sequence number.
    if (q.source) {
      emitScalarField(out, "    ", "source", q.source);
    }
  });
  const body = out.join("\n") + "\n";
  const stub =
    opts.domainsStub && !(manifest.domains && manifest.domains.length > 0)
      ? domainsStub(manifest.questions)
      : "";
  return stub + body;
}

// DOMAINS_STUB_PREAMBLE is rfp.domainsStubPreamble verbatim. The CLI's built
// manifests and the UI's must carry the same guidance, so the two copies of this
// text are asserted equal by lib/manifest.test.ts, which reads the Go source.
const DOMAINS_STUB_PREAMBLE = `#
# Optional domain routing. Uncomment and fill in to answer each group of
# questions within a stated requirement domain; leave it commented out to answer
# every question the same way. An entry needs a context, keywords, or both --
# uncommented but unfilled, the block is rejected rather than ignored. The most
# specific matching pattern wins, and patterns may use * and ?. A question id is
# matched first; an id that is a bare sequence number is matched against its
# source instead. The patterns below are the id prefixes and sources this
# document produced, not a suggested grouping.
#
`;

// domainsStub renders the commented-out `domains:` block written above a built
// manifest, mirroring rfp.domainsStub. It is comment text only: nothing is added
// to the manifest, so a built manifest still parses with no routing and an
// unedited stub cannot affect a run.
//
// Candidates come from what the questions carry, and every one is a key the
// resolver will actually consult for that question. A question with a non-digit
// id prefix ("C" for "C4") suggests that prefix as a glob; a structured number
// ("1.1") suggests its leading section ("1.*"). Only a question whose id is a
// bare sequence number, or which has no id at all, falls back to suggesting its
// source — the one case where the resolver consults source.
export function domainsStub(questions: BatchQuestion[]): string {
  interface Candidate {
    pattern: string;
    by: string;
    count: number;
  }
  const candidates: Candidate[] = [];
  const index = new Map<string, number>();
  const add = (pattern: string, by: string) => {
    // NUL-separated so a pattern can never run into the key it is joined to, as
    // rfp.domainsStub does.
    const key = `${by}\u0000${pattern}`;
    const at = index.get(key);
    if (at !== undefined) {
      candidates[at].count++;
      return;
    }
    index.set(key, candidates.length);
    candidates.push({ pattern, by, count: 1 });
  };
  for (const q of questions) {
    const pattern = idCandidate(q.id ?? "");
    if (pattern !== "") {
      add(pattern, "id prefix");
      continue;
    }
    const source = (q.source ?? "").trim();
    if (source !== "") add(source, "source");
  }

  const out: string[] = [DOMAINS_STUB_PREAMBLE, "# domains:\n"];
  if (candidates.length === 0) {
    out.push('#   - match: "*"   # no id prefixes or sources observed; edit this pattern\n');
    out.push('#     context: ""   # one sentence naming the requirement domain\n');
    out.push('#     keywords: []   # optional retrieval terms, a few at most\n');
    out.push("#\n");
    return out.join("");
  }
  candidates.forEach((c, i) => {
    out.push(`#   - match: ${quotePattern(c.pattern)}   # ${c.count} question(s) by ${c.by}\n`);
    // The placeholders are annotated once, on the first entry, so a long
    // candidate list stays readable.
    if (i === 0) {
      out.push('#     context: ""   # one sentence naming the requirement domain\n');
      out.push('#     keywords: []   # optional retrieval terms, a few at most\n');
      return;
    }
    out.push('#     context: ""\n');
    out.push('#     keywords: []\n');
  });
  out.push("#\n");
  return out.join("");
}

// idCandidate returns the glob to suggest for a question id, or "" when the id
// carries nothing to route on and the question's source should be suggested
// instead. Mirrors rfp.idCandidate.
//
// It mirrors the resolver's fallback condition exactly: "" is returned precisely
// when resolveDomain would consult the question's source. Without that, an id
// like "1.1" — which has no non-digit prefix, but is not a bare sequence number
// either — would be suggested by source, and the resolver would never look at a
// source for it, so uncommenting the entry would silently route nothing.
export function idCandidate(id: string): string {
  const trimmed = id.trim();
  if (trimmed === "" || isBareSequence(trimmed)) return "";
  const prefix = idPrefix(trimmed);
  if (prefix !== "") return `${prefix}*`;
  // A structured number such as "1.1" or "2-3". The separator is kept in the
  // pattern so "1.*" stays distinct from "10.*", which a bare "1*" would
  // swallow.
  const sep = trimmed.search(/[.\-_/ ]/);
  if (sep > 0) return `${trimmed.slice(0, sep + 1)}*`;
  // A digit run with some other suffix ("17a"): the digits are the only grouping
  // on offer.
  return `${/^[0-9]*/.exec(trimmed)?.[0] ?? ""}*`;
}

// idPrefix returns the leading run of non-digit characters in a question id, or
// "" when the id is empty or starts with a digit (the bare-sequence-number case
// the source fallback exists for). Mirrors rfp.idPrefix.
export function idPrefix(id: string): string {
  const trimmed = id.trim();
  const digit = trimmed.search(/[0-9]/);
  return digit < 0 ? trimmed : trimmed.slice(0, digit);
}

// quotePattern renders a candidate pattern the way the CLI's %q does: a
// double-quoted literal with the characters that would break out of it escaped.
// The candidates are id prefixes and sheet names, so the escapes that matter are
// the quote, the backslash, and stray control whitespace; anything more exotic
// would only ever appear inside a comment.
function quotePattern(pattern: string): string {
  return `"${escapeDoubleQuoted(pattern)}"`;
}

// emitScalarField appends `key: value`, using a literal block scalar when the
// value spans lines. A multi-line value used to be folded into a double-quoted
// scalar containing raw newlines, which reads back with its line breaks turned
// into spaces — lossy for exactly the values that want more than one line, a
// custom `prompt` and a domain `context`.
function emitScalarField(out: string[], indent: string, key: string, value: string): void {
  const block = blockScalar(value, indent);
  if (block) {
    out.push(`${indent}${key}: ${block.header}`);
    out.push(...block.lines);
    return;
  }
  out.push(`${indent}${key}: ${yamlScalar(value)}`);
}

// blockScalar renders a multi-line value as a literal block scalar, or returns
// null when the value is single-line or cannot be written as a block without
// changing it. Whitespace is what rules a value out: a trailing space on any
// line is invisible and unrecoverable, and an indented first line would need an
// explicit indentation indicator. Those values fall back to the quoted form,
// which escapes their newlines and so is still lossless.
function blockScalar(value: string, indent: string): { header: string; lines: string[] } | null {
  if (!value.includes("\n")) return null;
  if (value.includes("\r")) return null;

  const body = value.replace(/\n+$/, "").split("\n");
  if (body.some((line) => /[ \t]$/.test(line))) return null;
  const firstContent = body.find((line) => line !== "");
  if (firstContent === undefined || /^[ \t]/.test(firstContent)) return null;

  const trailing = value.length - value.replace(/\n+$/, "").length;
  const inner = indent + "  ";
  const lines = body.map((line) => (line === "" ? "" : inner + line));
  // Chomping: "|-" strips the final newline, "|" keeps exactly one, "|+" keeps
  // the blank lines that follow it.
  if (trailing === 0) return { header: "|-", lines };
  if (trailing === 1) return { header: "|", lines };
  return { header: "|+", lines: [...lines, ...Array(trailing - 1).fill("")] };
}

// AMBIGUOUS_PLAIN matches a string that, written unquoted, would be read back as
// something other than itself: a boolean, a null, or a number. The YAML 1.1
// spellings (yes/no/on/off) are included because the CLI's reader
// (gopkg.in/yaml.v3) resolves them as booleans and then refuses to decode one
// into a string field.
const AMBIGUOUS_PLAIN =
  /^(?:~|null|true|false|yes|no|on|off|[-+]?(?:\d[\d_]*(?:\.[\d_]*)?|\.\d[\d_]*)(?:[eE][-+]?\d+)?|[-+]?0[xX][0-9a-fA-F_]+|[-+]?0[oO][0-7_]+|[-+]?\.(?:inf|nan))$/i;

// yamlScalar quotes a scalar when a plain one would not read back unchanged:
// when it carries YAML punctuation or whitespace that matters, or when it looks
// like a non-string value. The quoted form escapes backslashes, quotes, and
// control whitespace, so a double-quoted value survives a real YAML reader.
export function yamlScalar(value: string): string {
  if (value === "") return '""';
  if (/[:#"'\\\n\r\t]|^[\s>|&*!?%@`-]|[\s]$|:\s/.test(value) || AMBIGUOUS_PLAIN.test(value)) {
    return `"${escapeDoubleQuoted(value)}"`;
  }
  return value;
}

function escapeDoubleQuoted(value: string): string {
  return value
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r")
    .replace(/\t/g, "\\t");
}