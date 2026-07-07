// Answer-review data contract carried over verbatim from rag-snap-ui so the
// later migration of the `answer batch` review experience can reuse it. These
// types are intentionally NOT wired into any shipped screen in this change
// (see the local-ui-app spec: "Type contract present and unused").

export interface QAItem {
  id: string;
  question: string;
  answer: string;
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
