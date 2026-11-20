import type { JudgeSummary, Judgment, QAFile, QAItem, ParsedQAFile } from "./types";

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
    ...(file.prompt ? { prompt: file.prompt } : {}),
    ...(isJudgeSummary(file.judge) ? { judge: file.judge } : {}),
    items,
  };
}

// toResultsFile re-emits a parsed file in the CLI's BatchOutput shape, so an
// exported file round-trips with `answer batch` output — including the prompt
// provenance and the judge summary when the run carried them.
export function toResultsFile(parsed: ParsedQAFile): QAFile {
  return {
    generated_at: parsed.generated_at,
    model: parsed.model,
    ...(parsed.prompt ? { prompt: parsed.prompt } : {}),
    ...(parsed.judge ? { judge: parsed.judge } : {}),
    results: parsed.items,
  };
}

// isJudged reports whether a verdict carries a score; a verdict with only an
// error means the row was not judged.
export function isJudged(j: Judgment | undefined): boolean {
  return !!j && !j.error && typeof j.confidence === "number" && j.confidence > 0;
}

function isJudgeSummary(v: unknown): v is JudgeSummary {
  if (typeof v !== "object" || v === null) return false;
  const s = v as Record<string, unknown>;
  return ["green", "yellow", "red", "not_judged"].every((k) => typeof s[k] === "number");
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

// RatingFilter is one bucket of the review filter: the three ratings plus rows the
// judge could not assess.
export type RatingFilter = "red" | "yellow" | "green" | "not_judged";

// Red first: the filter reads in the order a reviewer works.
export const RATING_FILTERS: RatingFilter[] = ["red", "yellow", "green", "not_judged"];

export function ratingFilterOf(item: QAItem): RatingFilter {
  const j = item.judgment;
  if (!isJudged(j) || !j?.rating) return "not_judged";
  return j.rating;
}

// filterByRating keeps the items whose rating is shown, each with its index in the
// full result set, so a card's number and spreadsheet row survive filtering.
export function filterByRating(
  items: QAItem[],
  shown: ReadonlySet<RatingFilter>
): { item: QAItem; i: number }[] {
  return items.map((item, i) => ({ item, i })).filter(({ item }) => shown.has(ratingFilterOf(item)));
}
