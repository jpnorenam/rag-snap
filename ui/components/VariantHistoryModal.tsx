"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import { restoreVariant, variantVersions, type PromptName, type PromptVersion } from "@/lib/api/prompts";

interface Props {
  slot: PromptName;
  // The variant whose history is shown.
  name: string;
  // Called after a successful restore so the parent can refresh its view.
  onRestored: (version: number) => void;
  onClose: () => void;
}

// PREVIEW_CHARS bounds each version's inline preview.
const PREVIEW_CHARS = 140;

// VariantHistoryModal lists a variant's version history newest-first and lets the
// user restore an earlier version (which the daemon appends as a new head). It is
// a focus-trapped p-modal mirroring ConfirmModal's dialog structure.
export default function VariantHistoryModal({ slot, name, onRestored, onClose }: Props) {
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  const [versions, setVersions] = useState<PromptVersion[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [restoring, setRestoring] = useState<number | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      setVersions(await variantVersions(slot, name));
    } catch (e) {
      setVersions(null);
      setLoadError(errorMessage(e));
    }
  }, [slot, name]);

  useEffect(() => {
    void load();
  }, [load]);

  const focusable = useCallback((): HTMLElement[] => {
    if (!dialogRef.current) return [];
    return Array.from(
      dialogRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea, input, select, [tabindex]:not([tabindex="-1"])'
      )
    );
  }, []);

  useEffect(() => {
    openerRef.current = document.activeElement as HTMLElement | null;
    const first = focusable()[0] ?? dialogRef.current;
    first?.focus();
    return () => openerRef.current?.focus();
  }, [focusable, versions]);

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

  const onRestore = async (version: number) => {
    setRestoring(version);
    setActionError(null);
    try {
      const updated = await restoreVariant(slot, name, version);
      onRestored(updated.version);
    } catch (e) {
      setActionError(errorMessage(e));
      setRestoring(null);
    }
  };

  const headVersion = versions && versions.length > 0 ? versions[versions.length - 1].version : 0;

  return (
    <div className="p-modal app-modal" onClick={onClose} onKeyDown={onKeyDown}>
      <div
        className="p-modal__dialog variant-history"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        ref={dialogRef}
        onClick={(e) => e.stopPropagation()}
      >
        <header className="p-modal__header">
          <h2 className="p-modal__title" id={titleId}>
            History — {name}
          </h2>
        </header>

        {actionError && (
          <div className="p-notification--negative" role="alert">
            <div className="p-notification__content">
              <p className="p-notification__message">{actionError}</p>
            </div>
          </div>
        )}

        {!versions && !loadError && <Spinner label="Loading history…" />}

        {loadError && (
          <div className="p-notification--negative" role="alert">
            <div className="p-notification__content">
              <p className="p-notification__message">{loadError}</p>
              <button type="button" className="p-button u-no-margin--bottom" onClick={() => void load()}>
                Retry
              </button>
            </div>
          </div>
        )}

        {versions && (
          <ol className="variant-history__list" aria-label={`Versions of ${name}, newest first`}>
            {[...versions].reverse().map((v) => {
              const isHead = v.version === headVersion;
              return (
                <li key={v.version} className="variant-history__item">
                  <div className="variant-history__meta">
                    <span className="variant-history__version">v{v.version}</span>
                    {isHead && <span className="variant-history__current">Current</span>}
                  </div>
                  <p className="variant-history__preview u-text--muted p-text--small">
                    {v.value.replace(/\s+/g, " ").trim().slice(0, PREVIEW_CHARS)}
                    {v.value.length > PREVIEW_CHARS ? "…" : ""}
                  </p>
                  <details className="variant-history__full">
                    <summary>View full prompt</summary>
                    <div className="p-code-snippet">
                      <pre className="p-code-snippet__block">
                        <code>{v.value}</code>
                      </pre>
                    </div>
                  </details>
                  {!isHead && (
                    <button
                      type="button"
                      className="p-button u-no-margin--bottom"
                      onClick={() => void onRestore(v.version)}
                      disabled={restoring !== null}
                    >
                      {restoring === v.version ? "Restoring…" : "Restore"}
                    </button>
                  )}
                </li>
              );
            })}
          </ol>
        )}

        <footer className="p-modal__footer">
          <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
            Close
          </button>
        </footer>
      </div>
    </div>
  );
}
