"use client";

import { useRef, useState } from "react";
import { isRedacted, type ConfigEntry } from "@/lib/api/config";

interface Props {
  entries: ConfigEntry[];
  // When false, the zone renders read-only: no edit or revert controls at all,
  // rather than controls that are certain to fail.
  writable: boolean;
  // Per-row save error from the daemon, keyed by config key.
  errors: Record<string, string>;
  // Key currently saving, if any.
  saving: string | null;
  onSave: (key: string, value: string) => void;
  onRevert: (entry: ConfigEntry) => void;
}

// ConfigTable lists the effective configuration with its layer provenance and,
// when writable, edits values in place. Saving writes the user layer — the CLI's
// semantic, where a user value overrides the package value. There is deliberately
// no "add key" control: the daemon rejects keys that do not already exist.
export default function ConfigTable({
  entries,
  writable,
  errors,
  saving,
  onSave,
  onRevert,
}: Props) {
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  // The pencil that opened the editor, so focus returns to it on save or cancel.
  const editorOpenerRef = useRef<HTMLButtonElement | null>(null);

  const startEdit = (entry: ConfigEntry, opener: HTMLButtonElement | null) => {
    editorOpenerRef.current = opener;
    setEditing(entry.key);
    // A redacted value is not the real one, so the editor starts empty rather
    // than seeding the field with the redaction marker.
    setDraft(isRedacted(entry) ? "" : entry.value);
  };

  const closeEdit = () => {
    setEditing(null);
    setDraft("");
    editorOpenerRef.current?.focus();
  };

  return (
    <div className="config-table__wrapper">
      <table className="config-table" aria-label="Configuration">
        {/* Column widths live here, not on the cells: the table is fixed-layout, so
            the columns are what size it, and the cell classes stay about styling. */}
        <colgroup>
          <col className="config-table__col--key" />
          <col className="config-table__col--value" />
          <col className="config-table__col--layer" />
          {writable && <col className="config-table__col--actions" />}
        </colgroup>

        <thead>
          <tr>
            <th scope="col">Key</th>
            <th scope="col">Value</th>
            <th scope="col">Layer</th>
            {writable && (
              <th scope="col">
                <span className="u-off-screen">Actions</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => {
            const isEditing = editing === entry.key;
            const error = errors[entry.key];
            const busy = saving === entry.key;

            return (
              <tr key={entry.key}>
                <td className="config-table__key">
                  <code>{entry.key}</code>
                </td>

                <td className="config-table__value">
                  {isEditing ? (
                    <div className={`p-form-validation ${error ? "is-error" : ""}`}>
                      <input
                        type="text"
                        className="p-form-validation__input"
                        value={draft}
                        autoFocus
                        aria-label={`Value for ${entry.key}`}
                        aria-invalid={error ? true : undefined}
                        disabled={busy}
                        onChange={(e) => setDraft(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Escape") {
                            e.preventDefault();
                            closeEdit();
                          }
                          if (e.key === "Enter") {
                            e.preventDefault();
                            onSave(entry.key, draft);
                          }
                        }}
                      />
                      {error && (
                        <p className="p-form-validation__message" role="alert">
                          {error}
                        </p>
                      )}
                      <div className="config-table__actions">
                        <button
                          type="button"
                          className="p-button--positive u-no-margin--bottom"
                          disabled={busy}
                          onClick={() => onSave(entry.key, draft)}
                        >
                          {busy ? "Saving…" : "Save"}
                        </button>
                        <button
                          type="button"
                          className="p-button u-no-margin--bottom"
                          disabled={busy}
                          onClick={closeEdit}
                        >
                          Cancel
                        </button>
                      </div>
                    </div>
                  ) : isRedacted(entry) ? (
                    // The daemon never sends the secret; there is nothing to reveal.
                    <span className="config-table__redacted" title="This value is not shown">
                      ••••
                    </span>
                  ) : (
                    <span className="config-table__shown">{entry.value || "—"}</span>
                  )}
                </td>

                <td className="config-table__layer">
                  <span
                    className={[
                      "p-chip",
                      "u-no-margin--bottom",
                      entry.layer === "user" ? "p-chip--positive" : "",
                    ]
                      .filter(Boolean)
                      .join(" ")}
                  >
                    <span className="p-chip__value">{entry.layer}</span>
                  </span>
                </td>

                {writable && (
                  <td className="config-table__controls">
                    {!isEditing && (
                      <>
                        <button
                          type="button"
                          className="p-button--base u-no-margin--bottom"
                          aria-label={`Edit ${entry.key}`}
                          onClick={(e) => startEdit(entry, e.currentTarget)}
                        >
                          <i className="p-icon--edit" aria-hidden="true" />
                        </button>
                        {entry.layer === "user" && (
                          <button
                            type="button"
                            className="p-button--base u-no-margin--bottom"
                            onClick={() => onRevert(entry)}
                          >
                            Revert
                          </button>
                        )}
                      </>
                    )}
                  </td>
                )}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
