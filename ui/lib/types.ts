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
