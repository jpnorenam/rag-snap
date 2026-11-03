"use client";

import Spinner from "@/components/common/Spinner";
import type { SheetInfo, SheetScopeValue } from "@/lib/api/answer";

// The three settings, in the order they are offered. Answer is the default, so a
// workbook left untouched is analysed exactly as it was before scopes existed.
const SCOPES: { value: SheetScopeValue; label: string }[] = [
  { value: "answer", label: "Answer" },
  { value: "context", label: "Context only" },
  { value: "exclude", label: "Exclude" },
];

// SheetScopeState is the inventory fetch for the chosen RFP. "none" means the document
// has no tabs to scope (a PDF, DOCX or CSV), which renders nothing.
export type SheetScopeState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "loaded"; sheets: SheetInfo[] }
  | { status: "none" };

// scopeNeedsAnswer reports whether a scope leaves nothing to answer, which the daemon
// rejects; the form stops it at the control first.
export function scopeNeedsAnswer(sheets: SheetInfo[], scope: Record<number, SheetScopeValue>): boolean {
  return sheets.length > 0 && !sheets.some((s) => (scope[s.position] ?? "answer") === "answer");
}

// scopeRequest turns the chosen settings into the triage request's "sheets" field,
// naming every tab by its name. Not by position: the server matches a key against tab
// names first, so in a workbook with tabs named "1" or "2" a position key could pick a
// different tab. Undefined when every tab is answered, so an untouched workbook sends
// no scope at all.
export function scopeRequest(
  sheets: SheetInfo[],
  scope: Record<number, SheetScopeValue>
): Record<string, SheetScopeValue> | undefined {
  if (sheets.every((s) => (scope[s.position] ?? "answer") === "answer")) return undefined;
  const out: Record<string, SheetScopeValue> = {};
  for (const s of sheets) out[s.name] = scope[s.position] ?? "answer";
  return out;
}

interface Props {
  filename: string;
  state: SheetScopeState;
  scope: Record<number, SheetScopeValue>;
  onChange: (position: number, value: SheetScopeValue) => void;
}

// SheetScope lists a spreadsheet RFP's tabs with a three-way setting per tab, so the
// operator chooses exactly which tabs are answered instead of asking the model to.
export default function SheetScope({ filename, state, scope, onChange }: Props) {
  if (state.status === "none") return null;

  if (state.status === "loading") {
    return (
      <div className="p-form__group">
        <p className="p-form__label">Tabs to analyse</p>
        <Spinner label={`Reading the tabs of ${filename}…`} />
      </div>
    );
  }

  if (state.status === "error") {
    return (
      <div className="p-form__group">
        <p className="p-form__label">Tabs to analyse</p>
        <div className="p-notification--caution" role="status">
          <div className="p-notification__content">
            <p className="p-notification__message">
              Could not list the tabs of {filename}: {state.message} Analysis can still run, over every tab.
            </p>
          </div>
        </div>
      </div>
    );
  }

  const nothingToAnswer = scopeNeedsAnswer(state.sheets, scope);
  return (
    <div className={["p-form__group", nothingToAnswer && "p-form-validation is-error"].filter(Boolean).join(" ")}>
      <p className="p-form__label" id="sheet-scope-label">
        Tabs to analyse
      </p>
      <p className="p-form-help-text" id="sheet-scope-help">
        <strong>Answer</strong>: questions are taken from the tab. <strong>Context only</strong>: the tab is sent as
        background, such as a read-me or sizing, and no questions are taken from it. <strong>Exclude</strong>: the tab
        is not sent at all. Use the instructions below for anything finer than a tab.
      </p>
      <div className="analysis-step__scroll">
        <table className="sheet-scope" aria-labelledby="sheet-scope-label" aria-describedby="sheet-scope-help">
          <thead>
            <tr>
              <th className="u-align--right">Tab</th>
              <th>Name</th>
              <th className="u-align--right">Rows</th>
              <th>Use</th>
            </tr>
          </thead>
          <tbody>
            {state.sheets.map((s) => {
              const value = scope[s.position] ?? "answer";
              return (
                <tr
                  key={s.position}
                  className={["sheet-scope__row", value !== "answer" && `sheet-scope__row--${value}`]
                    .filter(Boolean)
                    .join(" ")}
                >
                  <td className="u-align--right">{s.position}</td>
                  <td>{s.name}</td>
                  <td className="u-align--right">{s.data_rows}</td>
                  <td>
                    <div className="sheet-scope__choices" role="radiogroup" aria-label={`Use of tab ${s.position}, ${s.name}`}>
                      {SCOPES.map((opt) => (
                        <label key={opt.value} className="p-radio--inline">
                          <input
                            type="radio"
                            className="p-radio__input"
                            name={`sheet-scope-${s.position}`}
                            value={opt.value}
                            checked={value === opt.value}
                            onChange={() => onChange(s.position, opt.value)}
                          />
                          <span className="p-radio__label">{opt.label}</span>
                        </label>
                      ))}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {nothingToAnswer && (
        <p className="p-form-validation__message" role="alert">
          Set at least one tab to Answer: analysis needs questions to extract.
        </p>
      )}
    </div>
  );
}
