"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";

interface Props {
  // Dialog title, sentence case ("Cancel operation?").
  title: string;
  // What confirming does and what it costs, with the object named.
  children: React.ReactNode;
  // Verb-first label for the destructive button ("Cancel operation", "Delete").
  confirmLabel: string;
  // When set, the user must type this exact string before confirming — used for
  // the heavyweight deletions (a knowledge base name).
  confirmText?: string;
  // Label above the type-to-confirm input.
  confirmTextLabel?: string;
  onConfirm: () => void;
  onClose: () => void;
}

// ConfirmModal is the only confirmation surface in the app (never
// window.confirm). It has two variants: a plain confirm, and a type-to-confirm
// whose destructive action stays disabled until the input matches `confirmText`
// exactly. Focus moves into the dialog on open, is trapped while it is open,
// and returns to the triggering element on close.
export default function ConfirmModal({
  title,
  children,
  confirmLabel,
  confirmText,
  confirmTextLabel,
  onConfirm,
  onClose,
}: Props) {
  const titleId = useId();
  const inputId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<Element | null>(null);
  const [typed, setTyped] = useState("");

  const confirmable = !confirmText || typed === confirmText;

  // Move focus into the dialog on open and restore it to the opener on close.
  useEffect(() => {
    openerRef.current = document.activeElement;
    const first = dialogRef.current?.querySelector<HTMLElement>(FOCUSABLE);
    first?.focus();
    return () => {
      if (openerRef.current instanceof HTMLElement) openerRef.current.focus();
    };
  }, []);

  // Escape closes the dialog wherever focus happens to be.
  useEffect(() => {
    const onEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onEscape);
    return () => document.removeEventListener("keydown", onEscape);
  }, [onClose]);

  // Tab cycles within the dialog: focus never leaves an open modal.
  const onKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const items = dialogRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE);
      if (!items || items.length === 0) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    },
    []
  );

  return (
    <div className="p-modal" onClick={onClose} onKeyDown={onKeyDown}>
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
            {title}
          </h2>
          <button
            className="p-modal__close"
            aria-label="Close active modal"
            onClick={onClose}
            type="button"
          >
            Close
          </button>
        </header>

        {children}

        {confirmText && (
          <div className="p-form p-form--stacked">
            <div className="p-form__group">
              <label htmlFor={inputId}>
                {confirmTextLabel ?? `Type ${confirmText} to confirm`}
              </label>
              <input
                id={inputId}
                type="text"
                value={typed}
                autoComplete="off"
                onChange={(e) => setTyped(e.target.value)}
              />
            </div>
          </div>
        )}

        <footer className="p-modal__footer">
          <button className="p-button u-no-margin--bottom" type="button" onClick={onClose}>
            Go back
          </button>
          <button
            className="p-button--negative u-no-margin--bottom"
            type="button"
            disabled={!confirmable}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </footer>
      </div>
    </div>
  );
}

const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';
