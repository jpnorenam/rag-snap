"use client";

import type { QAItem } from "@/lib/types";
import SourcesList from "@/components/common/SourcesList";

interface Props {
  item: QAItem;
  index: number;
}

// The fixed "no context" answer the daemon emits when retrieval finds nothing.
const NO_CONTEXT_ANSWER =
  "The provided context does not contain enough information to answer this question.";

// QACard renders one question/answer pair. A failed or empty answer (or the
// fixed no-context response) is shown with a caution treatment rather than
// blank. The applied routing domain and the cited sources render only when the
// item carries them (results files written before citations have no sources).
export default function QACard({ item, index }: Props) {
  const domain = item.domain?.trim() ?? "";
  const answer = item.answer?.trim() ?? "";
  const isEmpty = answer === "";
  const isNoContext = answer === NO_CONTEXT_ANSWER;
  const caution = isEmpty || isNoContext;
  const classes = ["qa-card", caution ? "qa-card--caution" : ""].filter(Boolean).join(" ");
  const sources = item.sources ?? [];

  return (
    <article className={classes} id={`qa-${index + 1}`}>
      <h3 className="qa-card__question">
        <span className="qa-card__num u-text--muted">{index + 1}.</span> {item.question}
      </h3>
      {/* Which routing entry produced this answer, when the run recorded one.
          Metadata rather than answer text: it sits above the answer, muted, and
          outside the collapsible provenance section. Results carrying no domain
          — every file written before routing existed, and any question that
          matched no entry — render with nothing in its place. */}
      {domain !== "" && (
        <p className="qa-card__domain u-no-margin--bottom">
          Domain <code>{domain}</code>
        </p>
      )}
      {isEmpty ? (
        <p className="qa-card__answer u-text--muted">No answer was generated for this question.</p>
      ) : (
        <p className="qa-card__answer">{item.answer}</p>
      )}

      {sources.length > 0 && (
        <details className="qa-card__sources">
          <summary>Sources ({sources.length})</summary>
          {/* Batch answers carry no [n] markers, so only the labels are listed. */}
          <SourcesList
            sources={sources}
            showMarkers={false}
            showLabel={false}
            className="qa-card__sources-list"
          />
        </details>
      )}
    </article>
  );
}
