"use client";

import { useEffect, useId, useRef, useState } from "react";
import { errorMessage } from "@/lib/api/envelope";
import { setKnowledgeLabel } from "@/lib/api/knowledge";
import type { OperationView } from "@/lib/api/operations";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  name: string;
  currentLabel?: string;
  onSaved: (label: string, backfillOp: OperationView | null) => void;
  onClose: () => void;
}

// LABEL_PATTERN mirrors the daemon's knowledge-label format.
const LABEL_PATTERN = /^[a-z0-9][a-z0-9-]{0,31}$/;

// EditLabelModal changes a base's default knowledge label, optionally
// backfilling already-ingested chunks and sources that have no label yet.
// On error it keeps the modal open with the entered value preserved.
export default function EditLabelModal({ name, currentLabel, onSaved, onClose }: Props) {
  const titleId = useId();
  const labelId = useId();
  const backfillId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const { dialogRef, onKeyDown } = useModalDialog(onClose);
  const [label, setLabel] = useState(currentLabel ?? "");
  const [backfill, setBackfill] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const submit = async () => {
    const trimmed = label.trim();
    setError(null);
    if (!LABEL_PATTERN.test(trimmed)) {
      setError(
        "Use lowercase letters, digits, and hyphens; start with a letter or digit (max 32 characters)."
      );
      return;
    }
    setBusy(true);
    try {
      const op = await setKnowledgeLabel(name, trimmed, backfill);
      onSaved(trimmed, op);
    } catch (e) {
      // Keep the modal open with the entered value intact.
      setError(errorMessage(e));
      setBusy(false);
    }
  };

  return (
    <div className="p-modal app-modal" onClick={onClose} onKeyDown={onKeyDown}>
      <div
        className="p-modal__dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        ref={dialogRef}
        onClick={(e) => e.stopPropagation()}
      >
        <header className="p-modal__header">
          <h2 className="p-modal__title" id={titleId}>
            Edit default label
          </h2>
        </header>

        <form
          className="p-form p-form--stacked"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <div className={`p-form__group ${error ? "p-form-validation is-error" : ""}`}>
            <label htmlFor={labelId}>Default label</label>
            <input
              id={labelId}
              ref={inputRef}
              type="text"
              className={error ? "p-form-validation__input" : ""}
              value={label}
              autoComplete="off"
              onChange={(e) => setLabel(e.target.value)}
            />
            {error ? (
              <p className="p-form-validation__message">{error}</p>
            ) : (
              <p className="p-form-help-text">
                Sources ingested into <strong>{name}</strong> without an explicit label inherit
                it. Reference the tag in your prompts to prioritize this base&rsquo;s content.
              </p>
            )}
          </div>

          <div className="p-form__group">
            <label className="p-checkbox">
              <input
                type="checkbox"
                className="p-checkbox__input"
                id={backfillId}
                checked={backfill}
                onChange={(e) => setBackfill(e.target.checked)}
              />
              <span className="p-checkbox__label">Apply to existing sources</span>
            </label>
            <p className="p-form-help-text u-text--muted">
              Also label already-ingested chunks and sources that have no label yet. Sources
              ingested with an explicit label keep it.
            </p>
          </div>

          <footer className="p-modal__footer">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="p-button--positive u-no-margin--bottom" disabled={busy}>
              {busy ? "Saving…" : "Save label"}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
