"use client";

import type { QAItem } from "@/lib/types";

// SourceChunk is provenance for one answer. The batch results the daemon
// publishes today carry no per-question sources (chat.BatchResult is
// {id, question, answer}), so this is rendered only when present — a forward-
// compatible shape for when the API gains provenance (a separate change).
export interface SourceChunk {
  knowledge_base?: string;
  source_id?: string;
  score?: number;
}

// QAItemWithSources extends the review contract's QAItem with the optional
// provenance the review surface renders when available.
type QAItemWithSources = QAItem & { sources?: SourceChunk[] };

interface Props {
  item: QAItem;
  index: number;
}

// The fixed "no context" answer the daemon emits when retrieval finds nothing.
const NO_CONTEXT_ANSWER =
  "The provided context does not contain enough information to answer this question.";

// QACard renders one question/answer pair. A failed or empty answer (or the
// fixed no-context response) is shown with a caution treatment rather than
// blank. Sources render only when the item carries provenance.
export default function QACard({ item, index }: Props) {
  const withSources = item as QAItemWithSources;
  const answer = item.answer?.trim() ?? "";
  const isEmpty = answer === "";
  const isNoContext = answer === NO_CONTEXT_ANSWER;
  const caution = isEmpty || isNoContext;
  const classes = ["qa-card", caution ? "qa-card--caution" : ""].filter(Boolean).join(" ");
  const sources = withSources.sources ?? [];

  return (
    <article className={classes} id={`qa-${index + 1}`}>
      <h3 className="qa-card__question">
        <span className="qa-card__num u-text--muted">{index + 1}.</span> {item.question}
      </h3>
      {isEmpty ? (
        <p className="qa-card__answer u-text--muted">No answer was generated for this question.</p>
      ) : (
        <p className="qa-card__answer">{item.answer}</p>
      )}

      {sources.length > 0 && (
        <details className="qa-card__sources">
          <summary>Sources ({sources.length})</summary>
          <ul className="qa-card__sources-list">
            {sources.map((s, i) => (
              <li key={i}>
                {s.knowledge_base && <span className="p-chip"><span className="p-chip__value">{s.knowledge_base}</span></span>}
                {s.source_id && <span className="u-text--muted p-text--small"> {s.source_id}</span>}
                {typeof s.score === "number" && (
                  <span className="u-text--muted p-text--small"> · score {s.score.toFixed(3)}</span>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
    </article>
  );
}
