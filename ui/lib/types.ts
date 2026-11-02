// Answer-review data contract carried over verbatim from rag-snap-ui so the
// later migration of the `answer batch` review experience can reuse it. These
// types are intentionally NOT wired into any shipped screen in this change
// (see the local-ui-app spec: "Type contract present and unused").

/**
 * A retrieved document's citation, mirroring the daemon's knowledge.Citation:
 * a human-readable label and, when one was recorded at ingest, a link.
 * `locator` is reserved for section/cell references and is currently absent.
 */
export interface Citation {
  key: string;
  label: string;
  href?: string;
  locator?: string;
}

/**
 * A document an answer cites (chat.CitedSource): its citation plus the `n` of
 * the `[n]` marker the answer uses for it, the knowledge base it came from,
 * and its knowledge-label tag.
 */
export interface CitedSource extends Citation {
  marker: number;
  base: string;
  tag: string;
}

export interface QAItem {
  id: string;
  question: string;
  answer: string;
  /**
   * The documents the answer cited, in first-cited order. Optional because
   * results files written before citations existed do not carry it.
   */
  sources?: CitedSource[];
  /**
   * The `domains` entry that produced this answer, identified by its matched
   * pattern (chat.BatchResult.Domain, `domain` in the results JSON). Optional
   * because it is absent from every results file written before domain routing
   * existed, and from any run whose question matched no entry — the review
   * surface shows it only when it is there.
   */
  domain?: string;
}

export interface QAFile {
  generated_at: string;
  model: string;
  /** Some files use "results", spec says "result" — we handle both */
  results?: QAItem[];
  result?: QAItem[];
}

export interface ParsedQAFile {
  generated_at: string;
  model: string;
  items: QAItem[];
}
