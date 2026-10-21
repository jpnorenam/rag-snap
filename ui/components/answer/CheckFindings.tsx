"use client";

import { defects, warnings, type CheckFinding, type CheckResult } from "@/lib/api/answer";

interface Props {
  result: CheckResult;
  className?: string;
}

// FindingIcon marks a finding's severity with a shape as well as a colour.
//
// Colour alone would leave the distinction invisible to a viewer who cannot
// distinguish the two hues, and this is the one place in the flow where the
// difference decides whether the operator should stop: a defect will fail a run or
// contribute nothing, a warning may be entirely deliberate.
function FindingIcon({ severity }: { severity: CheckFinding["severity"] }) {
  const isDefect = severity === "defect";
  return (
    <svg
      className="check-findings__icon"
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
      {isDefect ? (
        <>
          <circle cx="12" cy="12" r="9" />
          <path d="M15 9l-6 6M9 9l6 6" />
        </>
      ) : (
        <>
          <path d="M12 3l9 16H3z" />
          <path d="M12 9v5M12 17h.01" />
        </>
      )}
    </svg>
  );
}

function FindingList({
  heading,
  findings,
  severity,
}: {
  heading: string;
  findings: CheckFinding[];
  severity: CheckFinding["severity"];
}) {
  if (findings.length === 0) return null;
  const isDefect = severity === "defect";
  return (
    <div
      className={[
        "check-findings__group",
        isDefect ? "check-findings__group--defect" : "check-findings__group--warning",
      ].join(" ")}
      // Only defects are announced: a reserved domain pattern is not an error and
      // interrupting a screen reader for it would be noise.
      role={isDefect ? "alert" : undefined}
    >
      <h4 className="check-findings__heading">{heading}</h4>
      <ul className="check-findings__list">
        {findings.map((f, i) => (
          <li key={`${f.code}-${f.subject ?? i}`} className="check-findings__item">
            <FindingIcon severity={severity} />
            <span>
              {/* The severity is named in words, not conveyed only by icon or colour, so
                  it survives a copy-paste into a ticket. */}
              <strong className="check-findings__severity">{isDefect ? "Defect" : "Warning"}</strong>{" "}
              {f.message}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// CheckFindings renders a manifest check: the counts, the per-domain routing table,
// which reference checks actually ran, and the findings at both severities.
//
// It is the single findings surface, used for a generated manifest and for one the
// user supplied, so a hand-written and a generated manifest are reported identically.
export default function CheckFindings({ result, className }: Props) {
  const defectList = defects(result);
  const warningList = warnings(result);
  const clean = result.findings.length === 0;

  return (
    <div className={["check-findings", className].filter(Boolean).join(" ")}>
      <p className="check-findings__summary">
        {result.questions} question{result.questions === 1 ? "" : "s"}, {result.domains} domain
        {result.domains === 1 ? "" : "s"}
        {clean ? " — no findings." : ""}
      </p>

      {result.domain_matches.length > 0 && (
        <div className="check-findings__routing">
          <h4 className="check-findings__heading">Domain routing</h4>
          {/* Wide on a narrow screen, so it scrolls inside its own wrapper rather
              than making the page scroll horizontally. */}
          <div className="check-findings__scroll">
            <table aria-label="Questions routed by each domain pattern">
              <thead>
                <tr>
                  <th>Pattern</th>
                  <th className="u-align--right">Questions</th>
                  <th>Single question routed</th>
                </tr>
              </thead>
              <tbody>
                {result.domain_matches.map((dm) => (
                  <tr key={dm.match} className={dm.count === 0 ? "check-findings__row--empty" : undefined}>
                    <td>
                      <code>{dm.match}</code>
                    </td>
                    <td className="u-align--right">{dm.count}</td>
                    {/* Shown only for an entry routing exactly one question, so a context
                        attached to the wrong question can be seen at a glance. */}
                    <td>
                      {dm.only_question && (
                        <>
                          <code>{dm.only_question.id}</code> {dm.only_question.text}
                          {dm.context_preview && (
                            <span className="p-text--small u-text--muted check-findings__context">
                              Context: {dm.context_preview}
                            </span>
                          )}
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Each reference check reports separately whether it ran. Saying nothing would
          imply a check passed when it never happened. */}
      {!result.references_checked && (
        <p className="p-notification--information u-no-margin--bottom">
          <span className="p-notification__content">
            <span className="p-notification__message">
              Knowledge base references were not checked — the knowledge store was unreachable.
            </span>
          </span>
        </p>
      )}
      {result.references_checked && !result.kapa_checked && (
        <p className="u-text--muted p-text--small">
          Kapa source group references were not checked: kapa is not configured on this daemon.
        </p>
      )}

      <FindingList heading="Would fail a run, or contribute nothing" findings={defectList} severity="defect" />
      <FindingList heading="Will run, but may not be intended" findings={warningList} severity="warning" />
    </div>
  );
}
