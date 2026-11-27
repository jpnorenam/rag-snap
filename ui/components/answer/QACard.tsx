"use client";

import type { ReactNode } from "react";
import { isJudged } from "@/lib/results";
import type { Judgment, QAItem } from "@/lib/types";
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
      {item.judgment && <Verdict judgment={item.judgment} row={index + 2} />}
      {isEmpty ? (
        <p className="qa-card__answer u-text--muted">No answer was generated for this question.</p>
      ) : (
        <p className="qa-card__answer">{item.answer}</p>
      )}
      {!isEmpty && item.unsourced && (
        <p className="qa-card__unsourced p-text--small">
          No sources: this answer cites none of the retrieved documents. Check it before using it.
        </p>
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

// VerdictKind names what a verdict asks of the reviewer. Each kind has its own icon
// shape and its own words, so the rating never rests on colour alone.
type VerdictKind = "ready" | "review" | "fix" | "unjudged";

const VERDICTS: Record<VerdictKind, { label: string }> = {
  ready: { label: "Green · paste as is" },
  review: { label: "Yellow · check or edit" },
  fix: { label: "Red · fix or answer" },
  unjudged: { label: "Not judged" },
};

function verdictKind(j: Judgment): VerdictKind {
  if (!isJudged(j)) return "unjudged";
  if (j.rating === "green") return "ready";
  if (j.rating === "yellow") return "review";
  return "fix";
}

// Verdict renders the judge's verdict between the question and the answer: the
// rating badge, the score, the spreadsheet row the reviewer works by, and the
// comment saying what to check. The comment is never collapsed — it is the point.
function Verdict({ judgment, row }: { judgment: Judgment; row: number }) {
  const kind = verdictKind(judgment);
  const text = kind === "unjudged" ? judgment.error : judgment.comment;
  const issues = kind === "unjudged" ? [] : (judgment.issues ?? []);
  return (
    <div className={["qa-verdict", `qa-verdict--${kind}`].join(" ")}>
      <p className="qa-verdict__line u-no-margin--bottom">
        <span className="qa-verdict__badge">
          <VerdictIcon kind={kind} />
          {VERDICTS[kind].label}
        </span>
        {kind !== "unjudged" && (
          <span className="qa-verdict__meta">
            Confidence {judgment.confidence}/5
            {judgment.pre_consistency_confidence ? ` (lowered from ${judgment.pre_consistency_confidence} by the consistency check)` : ""}
          </span>
        )}
        <span className="qa-verdict__meta">Spreadsheet row {row}</span>
      </p>
      {text && <p className="qa-verdict__comment u-no-margin--bottom">{text}</p>}
      {issues.length > 0 && (
        <p className="qa-verdict__issues u-no-margin--bottom">
          {issues.map((i) => i.replaceAll("_", " ")).join(" · ")}
        </p>
      )}
    </div>
  );
}

function VerdictIcon({ kind }: { kind: VerdictKind }) {
  const shape: Record<VerdictKind, ReactNode> = {
    // Circle with a tick.
    ready: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M8 12.5l2.5 2.5L16 9.5" />
      </>
    ),
    // Triangle with an exclamation mark.
    review: (
      <>
        <path d="M12 3.5L21.5 20h-19z" />
        <path d="M12 10v4.5M12 17.5v.5" />
      </>
    ),
    // Octagon with a cross.
    fix: (
      <>
        <path d="M8.3 3h7.4L21 8.3v7.4L15.7 21H8.3L3 15.7V8.3z" />
        <path d="M9 9l6 6M15 9l-6 6" />
      </>
    ),
    // Circle with a dash.
    unjudged: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M8 12h8" />
      </>
    ),
  };
  return (
    <svg
      className="qa-verdict__icon"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {shape[kind]}
    </svg>
  );
}
