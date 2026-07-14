"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";

interface Props {
  // Modal title (also the accessible name via aria-labelledby).
  title: string;
  // Body content: a sentence naming the object and stating consequences.
  children: React.ReactNode;
  // Confirm button label; verb-first and specific ("Cancel operation").
  confirmLabel: string;
  // When set, the confirm button stays disabled until the user types this exact
  // string (type-to-confirm variant, foundation §8). The input is labelled with
  // this name. When omitted, a plain confirm modal is rendered.
  confirmPhrase?: string;
  // Whether the confirm action is destructive (negative button styling).
  destructive?: boolean;
  // In-flight flag: disables the confirm button and swaps its label.
  busy?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}

// ConfirmModal is the shared confirmation dialog (foundation §6/§8): a
// focus-trapped p-modal with plain and type-to-confirm variants. It moves focus
// into the dialog on open, cycles Tab within it, restores focus on close, and
// closes on Escape or overlay click. Never use window.confirm.
export default function ConfirmModal({
  title,
  children,
  confirmLabel,
  confirmPhrase,
  destructive,
  busy,
  onConfirm,
  onClose,
}: Props) {
  const titleId = useId();
  const inputId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const [typed, setTyped] = useState("");

  // Restore focus to whatever was focused before the modal opened.
  const openerRef = useRef<HTMLElement | null>(null);

  const focusable = useCallback((): HTMLElement[] => {
    if (!dialogRef.current) return [];
    return Array.from(
      dialogRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea, input, select, [tabindex]:not([tabindex="-1"])'
      )
    );
  }, []);

  // Move focus into the dialog on open; restore it on close.
  useEffect(() => {
    openerRef.current = document.activeElement as HTMLElement | null;
    const first = focusable()[0] ?? dialogRef.current;
    first?.focus();
    return () => openerRef.current?.focus();
  }, [focusable]);

  // Escape closes; Tab cycles focus within the dialog (hand-rolled trap).
  const onKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
        return;
      }
      if (e.key !== "Tab") return;
      const items = focusable();
      if (items.length === 0) {
        e.preventDefault();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;
      if (e.shiftKey && active === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    },
    [focusable, onClose]
  );

  const gated = confirmPhrase !== undefined && typed !== confirmPhrase;
  const confirmDisabled = busy || gated;

  return (
    <div
      className="p-modal app-modal"
      onClick={onClose}
      onKeyDown={onKeyDown}
    >
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
        </header>

        {children}

        {confirmPhrase !== undefined && (
          <div className="p-form p-form--stacked">
            <div className="p-form__group">
              <label htmlFor={inputId}>
                Type <strong>{confirmPhrase}</strong> to confirm
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
          <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className={`u-no-margin--bottom ${destructive ? "p-button--negative" : "p-button--positive"}`}
            onClick={onConfirm}
            disabled={confirmDisabled}
          >
            {busy ? "Working…" : confirmLabel}
          </button>
        </footer>
      </div>
    </div>
  );
}
