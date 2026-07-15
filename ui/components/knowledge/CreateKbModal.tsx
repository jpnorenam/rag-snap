"use client";

import { useEffect, useId, useRef, useState } from "react";
import { errorMessage } from "@/lib/api/envelope";
import { createKnowledge } from "@/lib/api/knowledge";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  onCreated: (name: string) => void;
  onClose: () => void;
}

// NAME_PATTERN is a cheap client-side guard mirroring the daemon's naming rules
// (lowercase letters, digits, and hyphens). The daemon remains the authority.
const NAME_PATTERN = /^[a-z0-9][a-z0-9-]*$/;

// CreateKbModal is the create-knowledge-base dialog: a single validated name
// field. On error it keeps the modal open with the entered name preserved.
export default function CreateKbModal({ onCreated, onClose }: Props) {
  const titleId = useId();
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const { dialogRef, onKeyDown } = useModalDialog(onClose);
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const submit = async () => {
    const trimmed = name.trim();
    if (!NAME_PATTERN.test(trimmed)) {
      setError("Use lowercase letters, digits, and hyphens; start with a letter or digit.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await createKnowledge(trimmed);
      onCreated(trimmed);
    } catch (e) {
      // Keep the modal open with the entered name intact.
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
            Create knowledge base
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
            <label htmlFor={inputId}>Name</label>
            <input
              id={inputId}
              ref={inputRef}
              type="text"
              className={error ? "p-form-validation__input" : ""}
              value={name}
              autoComplete="off"
              onChange={(e) => setName(e.target.value)}
            />
            {error && <p className="p-form-validation__message">{error}</p>}
          </div>

          <footer className="p-modal__footer">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="p-button--positive u-no-margin--bottom" disabled={busy}>
              {busy ? "Creating…" : "Create"}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
