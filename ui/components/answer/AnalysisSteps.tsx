"use client";

import { useState } from "react";
import CheckFindings from "@/components/answer/CheckFindings";
import type {
  AnswerLengthMode,
  BatchManifest,
  CheckResult,
  OpenStackFlavour,
  Shortfall,
  ShortfallAcceptance,
  Triage,
} from "@/lib/api/answer";

// TriageStep shows what the first pass found, before the interview and before any
// manifest is generated. It exists so the operator can see which documents were read
// natively and which were converted, and abandon before spending a generation call.
export function TriageStep({
  triage,
  expiresAt,
  instructions,
  onBack,
  onContinue,
}: {
  triage: Triage;
  expiresAt: string;
  instructions: string;
  onBack: () => void;
  onContinue: () => void;
}) {
  return (
    <div className="analysis-step">
      <h3 className="p-heading--5">What the document appears to be</h3>

      {/* What every analysis call in this session is being told, so a surprising
          estimate can be traced to the instructions that produced it. */}
      {instructions && (
        <>
          <h4 className="analysis-step__heading">Working to</h4>
          <p className="analysis-instructions">{instructions}</p>
        </>
      )}

      <dl className="analysis-triage">
        {triage.title && (
          <>
            <dt>Document</dt>
            <dd>{triage.title}</dd>
          </>
        )}
        {triage.issuer && (
          <>
            <dt>Issuer</dt>
            <dd>{triage.issuer}</dd>
          </>
        )}
        <dt>Questions</dt>
        <dd>about {triage.estimated_questions}</dd>
        <dt>OpenStack</dt>
        <dd>
          {triage.openstack_in_scope ? "in scope" : "not in scope"}
          {triage.openstack_evidence && (
            <span className="u-text--muted p-text--small"> — {triage.openstack_evidence}</span>
          )}
        </dd>
        <dt>Fields per question</dt>
        <dd>
          {triage.multi_field_answers ? "more than one" : "one"}
          {triage.multi_field_detail && (
            <span className="u-text--muted p-text--small"> — {triage.multi_field_detail}</span>
          )}
        </dd>
        {triage.suggested_knowledge_bases.length > 0 && (
          <>
            <dt>Suggested bases</dt>
            <dd>{triage.suggested_knowledge_bases.join(", ")}</dd>
          </>
        )}
      </dl>

      {triage.sections.length > 0 && (
        <>
          <h4 className="analysis-step__heading">Sections found</h4>
          <ul className="analysis-sections">
            {triage.sections.map((s) => (
              <li key={s.name}>
                {s.name}
                {s.id_prefix && (
                  <>
                    {" "}
                    <code>{s.id_prefix}*</code>
                  </>
                )}
                {s.estimated_questions ? (
                  <span className="u-text--muted"> — about {s.estimated_questions} questions</span>
                ) : null}
              </li>
            ))}
          </ul>
        </>
      )}

      {triage.notes && <p className="u-text--muted p-text--small">{triage.notes}</p>}

      <h4 className="analysis-step__heading">How each document was read</h4>
      <div className="analysis-step__scroll">
        <table aria-label="How each document reached the model">
          <thead>
            <tr>
              <th>Document</th>
              <th>Format</th>
              <th>Read as</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {triage.report.map((r) => (
              <tr key={r.filename}>
                <td>{r.filename}</td>
                <td>{r.format}</td>
                <td>{r.path === "native" ? "natively" : "converted to text"}</td>
                <td className="u-text--muted p-text--small">{r.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="u-text--muted p-text--small">
        No model API accepts a spreadsheet or a word-processor document, so those are converted to
        table-preserving text first. A PDF is read natively.
      </p>

      {expiresAt && (
        <p className="u-text--muted p-text--small">
          This analysis session expires at {new Date(expiresAt).toLocaleTimeString()}. After that the
          documents must be uploaded again.
        </p>
      )}

      <div className="analysis-step__actions">
        <button type="button" className="p-button--base u-no-margin--bottom" onClick={onBack}>
          Discard
        </button>
        <button type="button" className="p-button--positive u-no-margin--bottom" onClick={onContinue}>
          Continue
        </button>
      </div>
    </div>
  );
}

// InterviewStep collects the answers that materially shape the manifest.
//
// The OpenStack control is rendered only when the triage reported OpenStack in scope —
// which is the whole reason the flow has two passes rather than one.
export function InterviewStep({
  triage,
  defaultWords,
  defaultLimit,
  busy,
  onBack,
  onSubmit,
}: {
  triage: Triage;
  defaultWords: number;
  defaultLimit: AnswerLengthMode;
  busy: boolean;
  onBack: () => void;
  onSubmit: (words: number, limit: AnswerLengthMode, flavour?: OpenStackFlavour) => void;
}) {
  const [words, setWords] = useState(String(defaultWords));
  const [limit, setLimit] = useState<AnswerLengthMode>(defaultLimit);
  const [flavour, setFlavour] = useState<OpenStackFlavour>("sunbeam");
  const [touched, setTouched] = useState(false);

  const parsed = Number(words.trim());
  const wordsValid = words.trim() !== "" && Number.isInteger(parsed) && parsed > 0;
  const showError = touched && !wordsValid;

  const submit = () => {
    setTouched(true);
    if (!wordsValid) return;
    onSubmit(parsed, limit, triage.openstack_in_scope ? flavour : undefined);
  };

  return (
    <div className="analysis-step">
      <h3 className="p-heading--5">How should answers be written?</h3>
      <form
        className="p-form p-form--stacked"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <div className={["p-form__group", showError ? "p-form-validation is-error" : ""].join(" ")}>
          <label className="p-form__label" htmlFor="analysis-words">
            Answer length in words
          </label>
          <div className="p-form__control">
            <input
              id="analysis-words"
              className="p-form-validation__input"
              type="number"
              min={1}
              inputMode="numeric"
              value={words}
              aria-invalid={showError}
              aria-describedby={showError ? "analysis-words-error" : "analysis-words-help"}
              onChange={(e) => setWords(e.target.value)}
              onBlur={() => setTouched(true)}
            />
            {showError ? (
              <p className="p-form-validation__message" id="analysis-words-error">
                Enter a positive whole number of words.
              </p>
            ) : (
              <p className="p-form-help-text" id="analysis-words-help">
                Applies to every answer in the batch.
              </p>
            )}
          </div>
        </div>

        <div className="p-form__group">
          <label className="p-form__label" htmlFor="analysis-limit">
            Is that a hard limit or a soft target?
          </label>
          <select
            id="analysis-limit"
            value={limit}
            onChange={(e) => setLimit(e.target.value as AnswerLengthMode)}
          >
            <option value="soft">Soft target — may be slightly exceeded</option>
            <option value="hard">Hard limit — must never be exceeded</option>
          </select>
        </div>

        {/* Rendered only when the RFP actually requires OpenStack. */}
        {triage.openstack_in_scope && (
          <div className="p-form__group">
            <label className="p-form__label" htmlFor="analysis-openstack">
              Which OpenStack offering should answers present?
            </label>
            <select
              id="analysis-openstack"
              value={flavour}
              onChange={(e) => setFlavour(e.target.value as OpenStackFlavour)}
            >
              <option value="sunbeam">Canonical OpenStack with Sunbeam</option>
              <option value="charmed">Charmed OpenStack</option>
            </select>
            <p className="p-form-help-text">
              Answers will describe this offering and will not present the other as the proposal.
            </p>
          </div>
        )}

        <div className="analysis-step__actions">
          <button
            type="button"
            className="p-button--base u-no-margin--bottom"
            onClick={onBack}
            disabled={busy}
          >
            Back
          </button>
          <button type="submit" className="p-button--positive u-no-margin--bottom" disabled={busy}>
            {busy ? "Generating…" : "Generate manifest"}
          </button>
        </div>
      </form>
    </div>
  );
}

// GeneratedReviewStep presents the generated manifest with its check findings, before
// the operator runs it.
//
// The prompt is shown in full rather than truncated: it is the field most likely to
// need an edit, and a preview would hide the part worth reading.
// ShortfallStep presents a question-count shortfall as a decision rather than a dead
// end. The same pair of counts arises from two passes splitting differently and from an
// extraction that left packed rows unsplit, so the per-section breakdown is shown: one
// section far below its estimate points at unsplit rows there. Caution, not negative,
// because nothing failed — and labelled in words, so it never relies on colour alone.
export function ShortfallStep({
  shortfall,
  busy,
  onContinue,
  onRerun,
  onStartAgain,
}: {
  shortfall: Shortfall;
  busy: boolean;
  onContinue: () => void;
  onRerun: () => void;
  onStartAgain: () => void;
}) {
  return (
    <div className="analysis-step">
      <div className="p-notification--caution" role="status">
        <div className="p-notification__content">
          <h5 className="p-notification__title">Fewer questions than estimated</h5>
          <p className="p-notification__message">
            Extraction produced {shortfall.produced} questions, but triage estimated about{" "}
            {shortfall.expected}. The extraction finished normally
            {shortfall.finish_reason ? ` (${shortfall.finish_reason})` : ""}, so this is not a budget
            problem: either the two passes split the document differently, or packed rows were left
            unsplit. Compare the sections below before deciding.
          </p>
          {shortfall.sections.length > 0 && (
            <div className="analysis-step__scroll">
              <table aria-label="Questions produced against the triage estimate, by section">
                <thead>
                  <tr>
                    <th>Section</th>
                    <th className="u-align--right">Produced</th>
                    <th className="u-align--right">Estimated</th>
                  </tr>
                </thead>
                <tbody>
                  {shortfall.sections.map((s) => (
                    <tr key={s.name}>
                      <td>
                        {s.name}
                        {s.id_prefix ? <code className="u-text--muted"> {s.id_prefix}*</code> : null}
                      </td>
                      <td className="u-align--right">{s.produced >= 0 ? s.produced : "unknown"}</td>
                      <td className="u-align--right">{s.estimated > 0 ? `about ${s.estimated}` : "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <div className="p-notification__meta">
          <div className="p-notification__actions">
            <button
              type="button"
              className="p-button--positive u-no-margin--bottom"
              onClick={onContinue}
              disabled={busy}
            >
              Continue with {shortfall.produced} questions
            </button>
            {/* At the top of the effort ladder there is nowhere higher to re-run. */}
            {shortfall.next_effort && (
              <button type="button" className="p-button u-no-margin--bottom" onClick={onRerun} disabled={busy}>
                Re-run with more reasoning ({shortfall.next_effort})
              </button>
            )}
            <button type="button" className="p-button u-no-margin--bottom" onClick={onStartAgain} disabled={busy}>
              Start again
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// PromptEditor is the model-written part of the prompt, editable, above the rules
// rag-cli appends, which are not. The rules sit outside the editable area, so no edit
// can remove, reorder or alter them; download and run join the two with joinPrompt.
export function PromptEditor({
  generated,
  appended,
  value,
  onChange,
  onRestore,
  disabled,
}: {
  generated: string;
  appended: string;
  value: string;
  onChange: (next: string) => void;
  onRestore: () => void;
  disabled: boolean;
}) {
  const edited = value !== generated;
  return (
    <>
      <div className="p-form__group">
        <label className="p-form__label" htmlFor="analysis-prompt-edit">
          RFP prompt (generated — editable){" "}
          {edited && (
            <span className="p-chip u-no-margin--bottom">
              <span className="p-chip__value">Edited</span>
            </span>
          )}
        </label>
        <textarea
          id="analysis-prompt-edit"
          className="analysis-prompt__editor"
          rows={14}
          value={value}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
        />
        <p className="p-form-help-text">
          Sent as the system prompt with every question, followed by the rules below.
        </p>
        {edited && (
          <button type="button" className="p-button u-no-margin--bottom" onClick={onRestore} disabled={disabled}>
            Restore generated prompt
          </button>
        )}
      </div>
      <h4 className="analysis-step__heading">Added by rag-cli — not editable here</h4>
      <div className="analysis-prompt" tabIndex={0} role="region" aria-label="Rules rag-cli appends to the prompt">
        <pre className="analysis-prompt__body">{appended}</pre>
      </div>
    </>
  );
}

export function GeneratedReviewStep({
  manifest,
  check,
  model,
  estimated,
  shortfallAccepted,
  promptGenerated,
  promptAppended,
  promptValue,
  onPromptChange,
  onPromptRestore,
  onBack,
  onDownload,
  onRun,
  running,
}: {
  manifest: BatchManifest;
  check: CheckResult;
  model: string;
  estimated: number;
  shortfallAccepted: ShortfallAcceptance | null;
  // The prompt's halves; null from a daemon that does not send them, in which case the
  // prompt is shown whole and read-only as before.
  promptGenerated: string | null;
  promptAppended: string | null;
  promptValue: string;
  onPromptChange: (next: string) => void;
  onPromptRestore: () => void;
  onBack: () => void;
  onDownload: () => void;
  onRun: () => void;
  running: boolean;
}) {
  return (
    <div className="analysis-step">
      <h3 className="p-heading--5">Generated manifest</h3>

      <dl className="analysis-triage">
        <dt>Questions</dt>
        <dd>
          {manifest.questions.length}
          {estimated > 0 && (
            <span className="u-text--muted"> (triage estimated about {estimated})</span>
          )}
          {shortfallAccepted && (
            <span className="u-text--muted">
              {" "}
              — accepted below the estimate; the downloaded manifest records it
            </span>
          )}
        </dd>
        <dt>Knowledge bases</dt>
        <dd>{(manifest.knowledge_bases ?? []).join(", ") || "none"}</dd>
        <dt>Domains</dt>
        <dd>{(manifest.domains ?? []).length}</dd>
        {model && (
          <>
            <dt>Generated by</dt>
            <dd>{model}</dd>
          </>
        )}
      </dl>

      <CheckFindings result={check} />

      {promptGenerated !== null && promptAppended !== null ? (
        <PromptEditor
          generated={promptGenerated}
          appended={promptAppended}
          value={promptValue}
          onChange={onPromptChange}
          onRestore={onPromptRestore}
          disabled={running}
        />
      ) : (
        <>
          <h4 className="analysis-step__heading">RFP prompt</h4>
          <p className="u-text--muted p-text--small">
            Sent as the system prompt with every question. The answer-length and gap-reporting rules at
            the end are added by rag-cli, not the model.
          </p>
          <div className="analysis-prompt" tabIndex={0} role="region" aria-label="Generated RFP prompt">
            <pre className="analysis-prompt__body">{manifest.prompt}</pre>
          </div>
        </>
      )}

      <div className="analysis-step__actions">
        <button
          type="button"
          className="p-button--base u-no-margin--bottom"
          onClick={onBack}
          disabled={running}
        >
          Back
        </button>
        {/* Downloadable even when the check found defects, so the operator can fix it
            by hand rather than being forced to regenerate. */}
        <button
          type="button"
          className="p-button u-no-margin--bottom"
          onClick={onDownload}
          disabled={running}
        >
          Download manifest
        </button>
        <button
          type="button"
          className="p-button--positive u-no-margin--bottom"
          onClick={onRun}
          disabled={running}
        >
          {running ? "Starting…" : "Run batch"}
        </button>
      </div>
    </div>
  );
}
