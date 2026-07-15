"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import ConfirmModal from "@/components/common/ConfirmModal";
import { errorMessage } from "@/lib/api/envelope";
import type { OperationView } from "@/lib/api/operations";
import {
  gdriveConnect,
  gdriveDisconnect,
  gdriveImport,
  gdriveResolve,
  gdriveStatus,
  type GdriveArchive,
  type GdriveStatus,
} from "@/lib/api/gdrive";
import { absoluteTime, relativeTime } from "@/lib/relativeTime";

interface Props {
  onStarted: (op: OperationView) => void;
  onClose: () => void;
}

type Step = "connect" | "locate" | "pick";

const POLL_INTERVAL_MS = 1500;

// formatBytes renders a byte count compactly (parity with the CLI's humanBytes).
function formatBytes(bytes: number): string {
  if (bytes < 0) return "";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${i === 0 ? n : n.toFixed(1)} ${units[i]}`;
}

// GdriveImport is the multi-step Google Drive import flow rendered inside the
// import modal: connect → locate → pick → import. It reaches parity with the CLI
// `k import --url` (folder select-all = --all, single-file URLs skip picking).
export default function GdriveImport({ onStarted, onClose }: Props) {
  const urlFieldId = useId();
  const forceId = useId();

  const [status, setStatus] = useState<GdriveStatus | null>(null);
  const [step, setStep] = useState<Step>("connect");
  const [waiting, setWaiting] = useState(false);
  const [connectError, setConnectError] = useState<string | null>(null);

  const [url, setUrl] = useState("");
  const [resolving, setResolving] = useState(false);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [archives, setArchives] = useState<GdriveArchive[]>([]);
  const [singleFile, setSingleFile] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [force, setForce] = useState(false);
  const [importing, setImporting] = useState(false);
  const [showDisconnect, setShowDisconnect] = useState(false);

  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const stepRef = useRef<Step>(step);
  stepRef.current = step;

  const stopPolling = useCallback(() => {
    if (pollRef.current !== null) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  const refreshStatus = useCallback(async () => {
    try {
      const s = await gdriveStatus();
      setStatus(s);
      if (s.connected) {
        setWaiting(false);
        setConnectError(null);
        stopPolling();
        if (stepRef.current === "connect") setStep("locate");
      } else if (s.error && !s.pending) {
        setWaiting(false);
        stopPolling();
        setConnectError(s.error);
      }
    } catch (e) {
      setConnectError(errorMessage(e));
    }
  }, [stopPolling]);

  // Initial status check, and cleanup any polling on unmount.
  useEffect(() => {
    void refreshStatus();
    return stopPolling;
  }, [refreshStatus, stopPolling]);

  const startPolling = useCallback(() => {
    stopPolling();
    pollRef.current = setInterval(() => void refreshStatus(), POLL_INTERVAL_MS);
  }, [refreshStatus, stopPolling]);

  const onConnect = async () => {
    setConnectError(null);
    try {
      const { consent_url } = await gdriveConnect();
      // Open consent in a new tab — never navigate the SPA away.
      window.open(consent_url, "_blank", "noopener,noreferrer");
      setWaiting(true);
      startPolling();
    } catch (e) {
      setConnectError(errorMessage(e));
    }
  };

  const cancelWaiting = () => {
    setWaiting(false);
    stopPolling();
  };

  const onDisconnect = async () => {
    try {
      await gdriveDisconnect();
      setShowDisconnect(false);
      setStep("connect");
      setArchives([]);
      setSelected(new Set());
      await refreshStatus();
    } catch (e) {
      setShowDisconnect(false);
      setConnectError(errorMessage(e));
    }
  };

  const onResolve = async () => {
    setResolveError(null);
    const u = url.trim();
    if (!/drive\.google\.com/.test(u)) {
      setResolveError("Enter a Google Drive folder or file URL.");
      return;
    }
    setResolving(true);
    try {
      const res = await gdriveResolve(u);
      setResolving(false);
      setArchives(res.archives);
      if (res.kind === "file") {
        setSingleFile(true);
        setSelected(new Set(res.archives.map((a) => a.id)));
        setStep("pick");
        return;
      }
      setSingleFile(false);
      if (res.archives.length === 0) {
        setResolveError("No .tar.gz archives found in that folder.");
        return;
      }
      setSelected(new Set());
      setStep("pick");
    } catch (e) {
      setResolving(false);
      setResolveError(errorMessage(e));
    }
  };

  const toggle = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const allSelected = archives.length > 0 && selected.size === archives.length;
  const someSelected = selected.size > 0 && !allSelected;

  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(archives.map((a) => a.id)));
  };

  const startImport = async () => {
    const chosen = archives.filter((a) => selected.has(a.id));
    if (chosen.length === 0) return;
    setImporting(true);
    setResolveError(null);
    const results = await Promise.allSettled(chosen.map((a) => gdriveImport(a, "", force)));
    const started = results
      .filter((r): r is PromiseFulfilledResult<OperationView> => r.status === "fulfilled")
      .map((r) => r.value);
    const failed = results.filter((r): r is PromiseRejectedResult => r.status === "rejected");

    if (started.length === 0) {
      setImporting(false);
      setResolveError(errorMessage(failed[0]?.reason));
      return;
    }
    // The parent tracks each operation and closes the modal; call after awaiting
    // so no state update lands on this component once it unmounts.
    started.forEach(onStarted);
    onClose();
  };

  const stepHeading =
    step === "connect" ? "Connect Google Drive" : step === "locate" ? "Locate archives" : "Pick archives";

  return (
    <div className="gdrive-import">
      <p className="u-off-screen" aria-live="polite">
        Step: {stepHeading}
      </p>

      {step !== "connect" && status?.connected && (
        <div className="gdrive-import__account">
          <span className="u-text--muted p-text--small">
            {status.account ? `Connected as ${status.account}` : "Connected to Google Drive"}
          </span>
          <button
            type="button"
            className="p-button--base u-no-margin--bottom"
            onClick={() => setShowDisconnect(true)}
          >
            Disconnect
          </button>
        </div>
      )}

      {step === "connect" && (
        <ConnectStep
          status={status}
          waiting={waiting}
          error={connectError}
          onConnect={() => void onConnect()}
          onCancelWaiting={cancelWaiting}
          onClose={onClose}
        />
      )}

      {step === "locate" && (
        <>
          <h3 className="p-heading--5">Locate archives</h3>
          <div className={`p-form__group ${resolveError ? "p-form-validation is-error" : ""}`}>
            <label htmlFor={urlFieldId}>Drive folder or file URL</label>
            <input
              id={urlFieldId}
              type="url"
              value={url}
              autoComplete="off"
              placeholder="https://drive.google.com/drive/folders/…"
              onChange={(e) => setUrl(e.target.value)}
            />
            {resolveError ? (
              <p className="p-form-validation__message">{resolveError}</p>
            ) : (
              <p className="p-form-help-text u-text--muted">
                A folder lists its archives to pick from; a single file link imports directly.
              </p>
            )}
          </div>
          <footer className="p-modal__footer">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="p-button--positive u-no-margin--bottom"
              disabled={resolving}
              onClick={() => void onResolve()}
            >
              {resolving ? "Resolving…" : "Resolve"}
            </button>
          </footer>
        </>
      )}

      {step === "pick" && (
        <>
          <h3 className="p-heading--5">{singleFile ? "Confirm import" : "Pick archives"}</h3>

          {singleFile ? (
            <p>
              Import <strong>{archives[0]?.name}</strong>
              {archives[0] && archives[0].size >= 0 ? ` (${formatBytes(archives[0].size)})` : ""} as a
              knowledge base.
            </p>
          ) : (
            <fieldset className="gdrive-import__list">
              <legend className="u-off-screen">Archives to import</legend>
              <label className="p-checkbox gdrive-import__selectall">
                <input
                  type="checkbox"
                  className="p-checkbox__input"
                  checked={allSelected}
                  ref={(el) => {
                    if (el) el.indeterminate = someSelected;
                  }}
                  onChange={toggleAll}
                />
                <span className="p-checkbox__label">Select all</span>
              </label>
              {archives.map((a) => (
                <div key={a.id} className="gdrive-import__item">
                  <label className="p-checkbox gdrive-import__item-check">
                    <input
                      type="checkbox"
                      className="p-checkbox__input"
                      checked={selected.has(a.id)}
                      onChange={() => toggle(a.id)}
                    />
                    <span className="p-checkbox__label">{a.name}</span>
                  </label>
                  <span className="u-text--muted p-text--small gdrive-import__meta">
                    {a.size >= 0 ? formatBytes(a.size) : ""}
                    {a.modified ? (
                      <>
                        {a.size >= 0 ? " · " : ""}
                        <time dateTime={a.modified} title={absoluteTime(a.modified)}>
                          {relativeTime(a.modified)}
                        </time>
                      </>
                    ) : null}
                  </span>
                </div>
              ))}
            </fieldset>
          )}

          <div className="p-form__group">
            <label className="p-checkbox">
              <input
                type="checkbox"
                className="p-checkbox__input"
                id={forceId}
                checked={force}
                onChange={(e) => setForce(e.target.checked)}
              />
              <span className="p-checkbox__label">Overwrite existing knowledge bases</span>
            </label>
            {force && (
              <p className="p-form-help-text u-text--muted">
                Existing bases with the same name will be replaced. This cannot be undone.
              </p>
            )}
          </div>

          {resolveError && (
            <div className="p-notification--negative" role="alert">
              <div className="p-notification__content">
                <p className="p-notification__message">{resolveError}</p>
              </div>
            </div>
          )}

          <footer className="p-modal__footer">
            {!singleFile && (
              <button
                type="button"
                className="p-button--base u-no-margin--bottom"
                onClick={() => setStep("locate")}
              >
                ← Back
              </button>
            )}
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="p-button--positive u-no-margin--bottom"
              disabled={importing || selected.size === 0}
              onClick={() => void startImport()}
            >
              {importing
                ? "Starting…"
                : selected.size <= 1
                  ? "Import archive"
                  : `Import ${selected.size} archives`}
            </button>
          </footer>
        </>
      )}

      {showDisconnect && (
        <ConfirmModal
          title="Disconnect Google Drive"
          confirmLabel="Disconnect"
          destructive
          onConfirm={() => void onDisconnect()}
          onClose={() => setShowDisconnect(false)}
        >
          <p>
            Deletes the stored authorization token from this machine. You will need to reconnect to
            import from Drive again.
          </p>
        </ConfirmModal>
      )}
    </div>
  );
}

// ConnectStep renders the first step: the not-configured info state, the connect
// card, the waiting state, or a recoverable error.
function ConnectStep({
  status,
  waiting,
  error,
  onConnect,
  onCancelWaiting,
  onClose,
}: {
  status: GdriveStatus | null;
  waiting: boolean;
  error: string | null;
  onConnect: () => void;
  onCancelWaiting: () => void;
  onClose: () => void;
}) {
  if (status === null) {
    return (
      <p className="gdrive-import__loading">
        <i className="p-icon--spinner u-animation--spin" aria-hidden="true" /> Checking Google Drive…
      </p>
    );
  }

  if (!status.configured) {
    return (
      <div className="p-notification--information">
        <div className="p-notification__content">
          <h3 className="p-notification__title">Google Drive import isn’t configured</h3>
          <p className="p-notification__message">
            Set <code>gdrive.client.id</code> and <code>gdrive.client.secret</code> (package config),
            then reopen this modal. See the Status page for the current configuration.
          </p>
        </div>
      </div>
    );
  }

  if (waiting) {
    return (
      <div className="gdrive-import__waiting">
        <p>
          <i className="p-icon--spinner u-animation--spin" aria-hidden="true" /> Waiting for Google
          authorization… Complete the consent screen in the new tab.
        </p>
        <footer className="p-modal__footer">
          <button type="button" className="p-button u-no-margin--bottom" onClick={onCancelWaiting}>
            Cancel
          </button>
        </footer>
      </div>
    );
  }

  return (
    <div className="gdrive-import__connect">
      {error && (
        <div className="p-notification--negative" role="alert">
          <div className="p-notification__content">
            <p className="p-notification__message">{error}</p>
          </div>
        </div>
      )}
      <div className="gdrive-import__connect-card">
        <GoogleDriveIcon />
        <p>Connect a Google account to import knowledge-base archives from Drive.</p>
        <button type="button" className="p-button--positive u-no-margin--bottom" onClick={onConnect}>
          {error ? "Try again" : "Connect Google Drive"}
        </button>
      </div>
      <p className="u-text--muted p-text--small">
        The authorization token is stored by the daemon on this machine and used only to read the
        archives you select.
      </p>
      <footer className="p-modal__footer">
        <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
          Cancel
        </button>
      </footer>
    </div>
  );
}

// GoogleDriveIcon is a decorative line-icon (the Drive triangle mark).
function GoogleDriveIcon() {
  return (
    <svg
      className="gdrive-import__icon"
      width="28"
      height="28"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M8 3h8l5 9-4 0-5-9z" />
      <path d="M3 18l4-7 4 7-4 0-4 0z" />
      <path d="M8 21l4-7 4 7-8 0z" />
    </svg>
  );
}
