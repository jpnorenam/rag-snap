import type { QAFile, QAItem, ParsedQAFile } from "./types";

// ResultsParseError is thrown when a results file (or an operation's results
// metadata) does not match the QAFile contract. Screens render its message as a
// validation error rather than an empty/broken review surface.
export class ResultsParseError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ResultsParseError";
  }
}

// normalizeQAFile collapses the QAFile contract's `result`/`results` key
// variants into a single `items` array (ParsedQAFile). It tolerates missing
// generated_at/model but requires a questions array of the right shape.
export function normalizeQAFile(file: QAFile): ParsedQAFile {
  const items = file.results ?? file.result;
  if (!Array.isArray(items)) {
    throw new ResultsParseError("results file has no `results` (or `result`) array");
  }
  for (const item of items) {
    if (!isQAItem(item)) {
      throw new ResultsParseError("a result item is missing its question or answer");
    }
  }
  return {
    generated_at: file.generated_at ?? "",
    model: file.model ?? "",
    items,
  };
}

// parseResultsJSON parses a results JSON string opened from disk and normalizes
// it. Malformed JSON or a shape mismatch throws ResultsParseError.
export function parseResultsJSON(text: string): ParsedQAFile {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    throw new ResultsParseError("file is not valid JSON");
  }
  if (typeof raw !== "object" || raw === null) {
    throw new ResultsParseError("results file must be a JSON object");
  }
  return normalizeQAFile(raw as QAFile);
}

function isQAItem(v: unknown): v is QAItem {
  if (typeof v !== "object" || v === null) return false;
  const item = v as Record<string, unknown>;
  return typeof item.question === "string" && typeof item.answer === "string";
}
