"use client";

import { useEffect, useId, useRef } from "react";
import type { Prompt, PromptVariantSummary } from "@/lib/api/prompts";

// PREVIEW_LINES is how much of the effective prompt the collapsed card shows;
// the rest is faded out by the .prompt-card__preview mask.
const PREVIEW_LINES = 4;

// CardEditor describes which editor a card is showing:
// - "active": edit the slot's current selection (write-through save; creates
//   and activates the `custom` variant when the default is active)
// - "variant": edit a named variant's head, appending a version without
//   touching the active pointer
// - "new": create a new variant, pre-filled from the current effective prompt
export type CardEditor =
  | { mode: "active" }
  | { mode: "variant"; variantName: string }
  | { mode: "new" };

// CardNotice is the transient per-card banner shown after a mutation, rendered
// inside the card the user acted on rather than at the top of the page.
export interface CardNotice {
  kind: "positive" | "negative";
  message: string;
}

interface Props {
  prompt: Prompt;
  // Variant summaries for generation slots (empty for source_rules); used for
  // the per-row version tag and the preview caption.
  variants: PromptVariantSummary[];
  // Human title and one-line description (docs/ux/05-prompts.md).
  title: string;
  description: string;
  // Generation slots (chat/answer) get variant management; source_rules does not.
  isGeneration: boolean;
  // Feedback for the last mutation on this card, if any.
  notice: CardNotice | null;

  // Editor state. draft/dirty/saving apply to whichever editor is open.
  editor: CardEditor | null;
  draft: string;
  dirty: boolean;
  saving: boolean;
  onDraftChange: (value: string) => void;
  onSave: () => void;
  onCancel: () => void;
  onReset: () => void;

  // New-variant name field (mode "new" only).
  newName: string;
  newNameError: string | null;
  onNewNameChange: (value: string) => void;

  // Collapsed-state actions.
  onEdit: () => void;
  onStartNewVariant: () => void;

  // Variant management (generation slots only). busyName names the variant an
  // activate/delete is in flight for ("" is the built-in default).
  busyName: string | null;
  onActivate: (variant: string) => void;
  onEditVariant: (variant: string) => void;
  onOpenHistory: (variant: string) => void;
  onDeleteVariant: (variant: string) => void;
}

// PromptCard renders one prompt slot. Collapsed: a radio group choosing the
// active prompt (variants first, built-in default last), a captioned preview of
// the current prompt, and Edit / New variant actions. Expanded: a monospace
// editor whose caption names exactly what is being edited. The source_rules
// guardrail card keeps the single-override edit/reset flow with no variant UI.
export default function PromptCard({
  prompt,
  variants,
  title,
  description,
  isGeneration,
  notice,
  editor,
  draft,
  dirty,
  saving,
  onDraftChange,
  onSave,
  onCancel,
  onReset,
  newName,
  newNameError,
  onNewNameChange,
  onEdit,
  onStartNewVariant,
  busyName,
  onActivate,
  onEditVariant,
  onOpenHistory,
  onDeleteVariant,
}: Props) {
  const headingId = useId();
  const editorId = useId();
  const nameId = useId();
  const radioGroupName = useId();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Focus the editor when it opens, so keyboard users land in it. The name
  // field takes focus instead in new-variant mode (autoFocus below).
  useEffect(() => {
    if (editor && editor.mode !== "new") textareaRef.current?.focus();
  }, [editor]);

  // Autogrow: let the textarea track its content instead of scrolling inside a
  // fixed box. The height is a genuinely dynamic value, the one case where an
  // inline style is sanctioned.
  useEffect(() => {
    const el = textareaRef.current;
    if (!editor || !el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [editor, draft]);

  const preview = prompt.value.split("\n").slice(0, PREVIEW_LINES).join("\n");
  const activeSummary = variants.find((v) => v.name === prompt.active) ?? null;

  const chipClasses = ["p-chip", prompt.customized ? "p-chip--positive" : ""]
    .filter(Boolean)
    .join(" ");
  const chipLabel = prompt.customized
    ? isGeneration && prompt.active
      ? prompt.active
      : "Customized"
    : "Default";

  // The collapsed Edit action: on an uncustomized generation slot it creates
  // the `custom` variant, so the label says what it does.
  const editLabel = isGeneration && !prompt.customized ? "Customize" : "Edit prompt";

  // What the open editor is editing, for its caption and save semantics.
  const editorCaption = (() => {
    if (!editor) return "";
    switch (editor.mode) {
      case "variant":
        return `Editing ${editor.variantName} — saving appends a new version without changing the active prompt.`;
      case "new":
        return "Starts from the current prompt. Give it a name and adjust the text.";
      case "active":
        if (isGeneration && !prompt.customized) {
          return 'Saving creates a variant named "custom" and activates it.';
        }
        return prompt.active
          ? `Editing the active variant (${prompt.active}) — saving appends a new version.`
          : "Editing the stored override.";
    }
  })();

  return (
    <section className="prompt-card" aria-labelledby={headingId}>
      <header className="prompt-card__header">
        <div>
          <h2 className="prompt-card__title" id={headingId}>
            {title}
          </h2>
          <p className="prompt-card__description u-text--muted p-text--small">{description}</p>
        </div>
        {/* The state is carried by the chip's text, not by colour alone. */}
        <span className={chipClasses}>
          <span className="p-chip__value">{chipLabel}</span>
        </span>
      </header>

      {/* Per-card feedback: rendered where the user acted, not at page top. */}
      {notice && (
        <div
          className={`p-notification--${notice.kind} prompt-card__notice`}
          role={notice.kind === "negative" ? "alert" : "status"}
        >
          <div className="p-notification__content">
            <p className="p-notification__message">{notice.message}</p>
          </div>
        </div>
      )}

      {!editor && (
        <>
          {isGeneration && (
            <fieldset className="prompt-card__variants" disabled={busyName !== null}>
              <legend className="prompt-card__variants-title">Active prompt</legend>
              {variants.map((v) => (
                <VariantRadioRow
                  key={v.name}
                  groupName={radioGroupName}
                  label={v.name}
                  versionTag={`v${v.versions}`}
                  checked={prompt.active === v.name}
                  busy={busyName === v.name}
                  onSelect={() => onActivate(v.name)}
                  onEdit={() => onEditVariant(v.name)}
                  onHistory={() => onOpenHistory(v.name)}
                  onDelete={() => onDeleteVariant(v.name)}
                />
              ))}
              <VariantRadioRow
                groupName={radioGroupName}
                label="Built-in default"
                checked={!prompt.active}
                busy={busyName === ""}
                onSelect={() => onActivate("")}
              />
            </fieldset>
          )}

          <p className="prompt-card__preview-caption">
            Current prompt
            {isGeneration && (
              <>
                {" — "}
                {prompt.active
                  ? `${prompt.active}${activeSummary ? ` v${activeSummary.versions}` : ""}`
                  : "built-in default"}
              </>
            )}
          </p>
          <div className="p-code-snippet prompt-card__preview">
            <pre className="p-code-snippet__block">
              <code>{preview}</code>
            </pre>
          </div>

          <div className="prompt-card__actions">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onEdit}>
              {editLabel}
            </button>
            {isGeneration && (
              <button
                type="button"
                className="p-button--base u-no-margin--bottom"
                onClick={onStartNewVariant}
              >
                New variant
              </button>
            )}
          </div>
        </>
      )}

      {editor && (
        <div className="p-form p-form--stacked">
          {editor.mode === "new" && (
            <div className={`p-form__group ${newNameError ? "p-form-validation is-error" : ""}`}>
              <label htmlFor={nameId}>Variant name</label>
              <input
                id={nameId}
                type="text"
                className={newNameError ? "p-form-validation__input" : ""}
                value={newName}
                autoFocus
                placeholder="presales-call"
                spellCheck={false}
                onChange={(e) => onNewNameChange(e.target.value)}
              />
              {newNameError && <p className="p-form-validation__message">{newNameError}</p>}
            </div>
          )}

          <div className="p-form__group">
            <label htmlFor={editorId}>
              {editor.mode === "new" ? "Prompt text" : title}
            </label>
            <p className="p-form-help-text">{editorCaption}</p>
            <textarea
              id={editorId}
              ref={textareaRef}
              className="prompt-card__editor"
              value={draft}
              rows={16}
              spellCheck={false}
              onChange={(e) => onDraftChange(e.target.value)}
              // Escape leaves edit mode; the parent confirms first when dirty.
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.stopPropagation();
                  onCancel();
                }
              }}
            />
          </div>

          {/* The built-in default stays available while editing, so a
              customization can be compared against it and copied from. */}
          <details className="prompt-card__default">
            <summary>View default prompt</summary>
            <div className="p-code-snippet">
              <pre className="p-code-snippet__block">
                <code>{prompt.default}</code>
              </pre>
            </div>
          </details>

          <div className="prompt-card__actions">
            <button
              type="button"
              className="p-button--positive u-no-margin--bottom"
              onClick={onSave}
              disabled={saving || (editor.mode === "new" ? draft.trim() === "" || newName.trim() === "" : !dirty)}
            >
              {saving
                ? editor.mode === "new"
                  ? "Creating…"
                  : "Saving…"
                : editor.mode === "new"
                  ? "Create variant"
                  : "Save"}
            </button>
            <button
              type="button"
              className="p-button u-no-margin--bottom"
              onClick={onCancel}
              disabled={saving}
            >
              Cancel
            </button>
            {editor.mode === "active" && prompt.customized && (
              <button
                type="button"
                className="p-button--base u-no-margin--bottom prompt-card__reset"
                onClick={onReset}
                disabled={saving}
              >
                Reset to default
              </button>
            )}
          </div>
        </div>
      )}
    </section>
  );
}

interface VariantRadioRowProps {
  groupName: string;
  label: string;
  // Head-version tag ("v3"); omitted for the built-in default row.
  versionTag?: string;
  checked: boolean;
  busy: boolean;
  onSelect: () => void;
  // Edit/History/Delete are omitted for the built-in default row.
  onEdit?: () => void;
  onHistory?: () => void;
  onDelete?: () => void;
}

// VariantRadioRow is one choice in a slot's active-prompt radio group: a native
// radio (selection activates immediately), the variant's name and head-version
// tag, and quiet per-variant actions. The active variant's Delete is disabled
// with the reason in its accessible name, not a tooltip.
function VariantRadioRow({
  groupName,
  label,
  versionTag,
  checked,
  busy,
  onSelect,
  onEdit,
  onHistory,
  onDelete,
}: VariantRadioRowProps) {
  return (
    <div className="prompt-card__variant-row">
      <label className="p-radio prompt-card__variant-choice">
        <input
          type="radio"
          className="p-radio__input"
          name={groupName}
          checked={checked}
          onChange={onSelect}
          aria-label={versionTag ? `${label}, ${versionTag}` : label}
        />
        <span className="p-radio__label" aria-hidden="true">
          <span className="prompt-card__variant-name">{label}</span>
          {versionTag && <span className="prompt-card__variant-version">{versionTag}</span>}
          {busy && <span className="prompt-card__variant-version">activating…</span>}
        </span>
      </label>
      {(onEdit || onHistory || onDelete) && (
        <div className="prompt-card__variant-actions">
          {onHistory && (
            <button
              type="button"
              className="p-button--base u-no-margin--bottom p-text--small"
              onClick={onHistory}
              aria-label={`History of ${label}`}
            >
              History
            </button>
          )}
          {onEdit && (
            <button
              type="button"
              className="p-button--base u-no-margin--bottom p-text--small"
              onClick={onEdit}
              aria-label={`Edit ${label}`}
            >
              Edit
            </button>
          )}
          {onDelete && (
            <button
              type="button"
              className="p-button--base u-no-margin--bottom p-text--small"
              onClick={onDelete}
              disabled={checked || busy}
              aria-label={
                checked
                  ? `Delete ${label} — activate another prompt first`
                  : `Delete ${label}`
              }
            >
              Delete
            </button>
          )}
        </div>
      )}
    </div>
  );
}
