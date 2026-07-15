"use client";

import { useCallback, useEffect, useRef } from "react";

// useModalDialog provides the shared focus behaviour for a modal dialog: it moves
// focus into the dialog on open, cycles Tab within it (hand-rolled trap, no
// dependency), restores focus to the opener on close, and closes on Escape. The
// returned ref goes on the `.p-modal__dialog` element and onKeyDown on the modal
// overlay (foundation §9). Overlay-click close is wired by the caller.
export function useModalDialog(onClose: () => void) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  const focusable = useCallback((): HTMLElement[] => {
    if (!dialogRef.current) return [];
    return Array.from(
      dialogRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea, input:not([disabled]), select, [tabindex]:not([tabindex="-1"])'
      )
    );
  }, []);

  useEffect(() => {
    openerRef.current = document.activeElement as HTMLElement | null;
    const first = focusable()[0] ?? dialogRef.current;
    first?.focus();
    return () => openerRef.current?.focus();
  }, [focusable]);

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

  return { dialogRef, onKeyDown };
}
