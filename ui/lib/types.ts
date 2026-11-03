// Answer-review data contract carried over verbatim from rag-snap-ui so the
// later migration of the `answer batch` review experience can reuse it. These
// types are intentionally NOT wired into any shipped screen in this change
// (see the local-ui-app spec: "Type contract present and unused").

export interface QAItem {
  id: string;
  question: string;
  answer: string;
  /**
   * The `domains` entry that produced this answer, identified by its matched
   * pattern (chat.BatchResult.Domain, `domain` in the results JSON). Optional
   * because it is absent from every results file written before domain routing
   * existed, and from any run whose question matched no entry — the review
   * surface shows it only when it is there.
   */
  domain?: string;
  /**
   * The judge's verdict on this answer (chat.Judgment). Absent from every file
   * written before judging existed and from any run that was not judged, so the
   * review surface shows a verdict only when it is there.
   */
  judgment?: Judgment;
}

/** A judge rating: green = paste as is, yellow = check or edit, red = fix or answer. */
export type JudgeRating = "green" | "yellow" | "red";

/**
 * A verdict on one answer. A row the judge could not assess carries only
 * `error` — no confidence and no rating — so "not judged" is never a score.
 */
export interface Judgment {
  confidence?: number;
  rating?: JudgeRating;
  issues?: string[];
  comment?: string;
  error?: string;
  /** The confidence before the consistency pass lowered it; present only when it did. */
  pre_consistency_confidence?: number;
}

/** One side of a contradiction: a spreadsheet row and the phrase in its answer that conflicts. */
export interface ConsistencyRow {
  row: number;
  phrase: string;
}

/**
 * A cross-answer finding (chat.ConsistencyFinding). A broad one names more rows than
 * a contradiction plausibly spans: it is listed for the reviewer but applied to no row.
 */
export interface ConsistencyFinding {
  rows: ConsistencyRow[];
  comment: string;
  broad?: boolean;
}

/** The run-level account of judging (chat.JudgeSummary, `judge` in the results JSON). */
export interface JudgeSummary {
  model: string;
  green: number;
  yellow: number;
  red: number;
  not_judged: number;
  consistency: boolean;
  consistency_error?: string;
  /** Every finding the consistency pass kept, recorded once for the run. */
  consistency_findings?: ConsistencyFinding[];
  /** Findings dropped because their quotes were not in the answers they named. */
  consistency_discarded?: number;
}

export interface QAFile {
  generated_at: string;
  model: string;
  /** The prompt provenance ("variant@version"), when the run recorded one. */
  prompt?: string;
  /** Present only on a judged run. */
  judge?: JudgeSummary;
  /** Some files use "results", spec says "result" — we handle both */
  results?: QAItem[];
  result?: QAItem[];
}

export interface ParsedQAFile {
  generated_at: string;
  model: string;
  prompt?: string;
  judge?: JudgeSummary;
  items: QAItem[];
}
