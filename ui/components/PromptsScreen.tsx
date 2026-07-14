"use client";

import { useCallback, useEffect, useState } from "react";
import Header from "@/components/Header";
import PromptCard from "@/components/PromptCard";
import ConfirmModal from "@/components/common/ConfirmModal";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import { listPrompts, resetPrompt, savePrompt, type Prompt, type PromptName } from "@/lib/api/prompts";
import { useUnsavedGuard } from "@/lib/useUnsavedGuard";

// PROMPT_LABELS gives each template its human title and one-line description
// (docs/ux/05-prompts.md). The daemon returns the prompts already in the order
// the CLI's `prompt init` presents them, so the page renders them as they come.
const PROMPT_LABELS: Record<PromptName, { title: string; description: string }> = {
  chat_system_prompt: {
    title: "Chat system prompt",
    description: "Sets the assistant's behaviour in interactive chat.",
  },
  answer_system_prompt: {
    title: "Answer system prompt",
    description: "Used for batch answering (RFPs).",
  },
  source_rules: {
    title: "Source rules",
    description: "Rules for how retrieved sources are cited and used.",
  },
};

// SAVED_MESSAGE states when a saved prompt takes effect. The daemon resolves
// prompts when a chat session or batch run *starts*, so work already running
// keeps the prompts it began with — this sentence must keep saying that.
const SAVED_MESSAGE = "Prompt saved. New chats and batch runs will use it.";

// Notice is the transient banner shown after a mutation.
interface Notice {
  kind: "positive" | "negative";
  message: string;
}

// Pending is the action held behind a confirm dialog.
type Pending =
  | { kind: "reset"; name: PromptName }
  | { kind: "discard"; next: PromptName | null };

export default function PromptsScreen() {
  const [prompts, setPrompts] = useState<Prompt[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [editing, setEditing] = useState<PromptName | null>(null);
  const [draft, setDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);

  const current = prompts?.find((p) => p.name === editing) ?? null;
  const dirty = current !== null && draft !== current.value;

  // Guard both exits from the page while an edit is unsaved.
  const { pendingHref, confirmNavigation, cancelNavigation } = useUnsavedGuard(dirty);

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      setPrompts(await listPrompts());
    } catch (e) {
      setPrompts(null);
      setLoadError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // openEditor switches edit mode to `name` (or closes it when null), confirming
  // first if the open card has unsaved changes.
  const openEditor = (name: PromptName | null) => {
    if (dirty) {
      setPending({ kind: "discard", next: name });
      return;
    }
    applyEditor(name);
  };

  const applyEditor = (name: PromptName | null) => {
    setNotice(null);
    setEditing(name);
    setDraft(name ? (prompts?.find((p) => p.name === name)?.value ?? "") : "");
  };

  // replace swaps one prompt in the list with the daemon's updated view, so the
  // chip and preview reflect what the daemon now holds.
  const replace = (updated: Prompt) => {
    setPrompts((list) => list?.map((p) => (p.name === updated.name ? updated : p)) ?? [updated]);
  };

  const onSave = async () => {
    if (!current || !dirty) return;
    setSaving(true);
    setNotice(null);
    try {
      replace(await savePrompt(current.name, draft));
      setEditing(null);
      setDraft("");
      setNotice({ kind: "positive", message: SAVED_MESSAGE });
    } catch (e) {
      // Keep the card open and the draft intact: a failed save must never cost
      // the user their text.
      setNotice({ kind: "negative", message: errorMessage(e) });
    } finally {
      setSaving(false);
    }
  };

  const onReset = async (name: PromptName) => {
    setSaving(true);
    setNotice(null);
    try {
      replace(await resetPrompt(name));
      setEditing(null);
      setDraft("");
      setNotice({ kind: "positive", message: "Prompt reset to its default. New chats and batch runs will use it." });
    } catch (e) {
      setNotice({ kind: "negative", message: errorMessage(e) });
    } finally {
      setSaving(false);
      setPending(null);
    }
  };

  const onConfirmPending = () => {
    if (!pending) return;
    if (pending.kind === "reset") {
      void onReset(pending.name);
      return;
    }
    // Discarding unsaved edits, then continuing where the user was headed.
    applyEditor(pending.next);
    setPending(null);
  };

  return (
    <>
      <Header title="Prompts" />
      <main className="app-main prompts">
        <p className="prompts__intro u-text--muted">
          These templates drive the assistant. Saved prompts are held by the daemon and used by
          chat sessions, batch runs, and the <code>rag-cli.rag prompt init</code> command.
        </p>

        {notice && (
          <div
            className={`p-notification--${notice.kind}`}
            role={notice.kind === "negative" ? "alert" : "status"}
          >
            <div className="p-notification__content">
              <p className="p-notification__message">{notice.message}</p>
            </div>
          </div>
        )}

        {/* Error: editing is blocked while the prompts cannot be loaded. */}
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

        {/* Loading: fixed-height skeletons, so the cards do not jump when the
            real content lands. */}
        {!prompts && !loadError && (
          <div aria-live="polite">
            <Spinner label="Loading prompts…" />
            <div className="prompts__skeletons" aria-hidden="true">
              <div className="prompt-card prompt-card--skeleton" />
              <div className="prompt-card prompt-card--skeleton" />
              <div className="prompt-card prompt-card--skeleton" />
            </div>
          </div>
        )}

        {/* There is no empty state: the three prompts always exist, defaulted. */}
        {prompts?.map((prompt) => (
          <PromptCard
            key={prompt.name}
            prompt={prompt}
            title={PROMPT_LABELS[prompt.name].title}
            description={PROMPT_LABELS[prompt.name].description}
            editing={editing === prompt.name}
            draft={editing === prompt.name ? draft : ""}
            dirty={editing === prompt.name && dirty}
            saving={saving && editing === prompt.name}
            onEdit={() => openEditor(prompt.name)}
            onDraftChange={setDraft}
            onSave={() => void onSave()}
            onCancel={() => openEditor(null)}
            onReset={() => setPending({ kind: "reset", name: prompt.name })}
          />
        ))}
      </main>

      {pending?.kind === "reset" && (
        <ConfirmModal
          title="Reset to default"
          confirmLabel="Reset to default"
          destructive
          busy={saving}
          onConfirm={onConfirmPending}
          onClose={() => setPending(null)}
        >
          <p>
            Replaces your customized <strong>{PROMPT_LABELS[pending.name].title.toLowerCase()}</strong>{" "}
            with the built-in default. New chats and batch runs will use the default.
          </p>
        </ConfirmModal>
      )}

      {pending?.kind === "discard" && (
        <ConfirmModal
          title="Discard unsaved changes?"
          confirmLabel="Discard changes"
          destructive
          onConfirm={onConfirmPending}
          onClose={() => setPending(null)}
        >
          <p>Your edits to this prompt have not been saved. Leaving the editor discards them.</p>
        </ConfirmModal>
      )}

      {pendingHref && (
        <ConfirmModal
          title="Discard unsaved changes?"
          confirmLabel="Leave and discard"
          destructive
          onConfirm={confirmNavigation}
          onClose={cancelNavigation}
        >
          <p>
            You have unsaved changes to a prompt. Leaving this page discards them.
          </p>
        </ConfirmModal>
      )}
    </>
  );
}
