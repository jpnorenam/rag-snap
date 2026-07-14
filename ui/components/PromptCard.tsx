"use client";

import { useEffect, useId, useRef } from "react";
import type { Prompt } from "@/lib/api/prompts";

// PREVIEW_LINES is how much of the effective prompt the collapsed card shows;
// the rest is faded out by the .prompt-card__preview mask.
const PREVIEW_LINES = 4;

interface Props {
  prompt: Prompt;
  // Human title and one-line description (docs/ux/05-prompts.md).
  title: string;
  description: string;
  editing: boolean;
  // Draft text while editing; ignored when collapsed.
  draft: string;
  dirty: boolean;
  saving: boolean;
  onEdit: () => void;
  onDraftChange: (value: string) => void;
  onSave: () => void;
  onCancel: () => void;
  onReset: () => void;
}

// PromptCard renders one prompt template: collapsed, a preview of the effective
// text with a Default/Customized chip; expanded, a monospace editor with the
// built-in default available for comparison, plus save/cancel/reset actions.
export default function PromptCard({
  prompt,
  title,
  description,
  editing,
  draft,
  dirty,
  saving,
  onEdit,
  onDraftChange,
  onSave,
  onCancel,
  onReset,
}: Props) {
  const headingId = useId();
  const editorId = useId();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Focus the editor when the card opens, so keyboard users land in it.
  useEffect(() => {
    if (editing) textareaRef.current?.focus();
  }, [editing]);

  // Autogrow: let the textarea track its content instead of scrolling inside a
  // fixed box. The height is a genuinely dynamic value, the one case where an
  // inline style is sanctioned.
  useEffect(() => {
    const el = textareaRef.current;
    if (!editing || !el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [editing, draft]);

  const preview = prompt.value.split("\n").slice(0, PREVIEW_LINES).join("\n");

  const chipClasses = ["p-chip", prompt.customized ? "p-chip--positive" : ""]
    .filter(Boolean)
    .join(" ");

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
          <span className="p-chip__value">{prompt.customized ? "Customized" : "Default"}</span>
        </span>
      </header>

      {!editing && (
        <>
          <div className="p-code-snippet prompt-card__preview">
            <pre className="p-code-snippet__block">
              <code>{preview}</code>
            </pre>
          </div>
          <div className="prompt-card__actions">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onEdit}>
              Edit
            </button>
          </div>
        </>
      )}

      {editing && (
        <div className="p-form p-form--stacked">
          <div className="p-form__group">
            <label htmlFor={editorId}>{title}</label>
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
              disabled={!dirty || saving}
            >
              {saving ? "Saving…" : "Save"}
            </button>
            <button
              type="button"
              className="p-button u-no-margin--bottom"
              onClick={onCancel}
              disabled={saving}
            >
              Cancel
            </button>
            {prompt.customized && (
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
