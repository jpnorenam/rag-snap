"use client";

import { useCallback, useEffect, useState } from "react";
import Header from "@/components/Header";
import PromptCard, { type CardEditor, type CardNotice } from "@/components/PromptCard";
import VariantHistoryModal from "@/components/VariantHistoryModal";
import ConfirmModal from "@/components/common/ConfirmModal";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import {
  activatePrompt,
  createVariant,
  deleteVariant,
  GENERATION_SLOTS,
  getVariant,
  isGenerationSlot,
  listPrompts,
  listVariants,
  resetPrompt,
  savePrompt,
  saveVariant,
  VARIANT_NAME_PATTERN,
  type Prompt,
  type PromptName,
  type PromptVariantSummary,
} from "@/lib/api/prompts";
import { useUnsavedGuard } from "@/lib/useUnsavedGuard";

// PROMPT_LABELS gives each slot its human title and one-line description
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

// Notice is a per-card banner: it renders inside the card the user acted on, so
// the feedback appears where they are looking rather than at the top of a long
// page.
interface Notice extends CardNotice {
  slot: PromptName;
}

// Editor identifies the one open editor: which card, and what it edits.
interface Editor {
  slot: PromptName;
  card: CardEditor;
  // baseline is the text the draft is compared against for dirtiness. For a
  // new-variant fork it is the effective prompt the editor was seeded with.
  baseline: string;
}

// Pending is the action held behind a confirm dialog.
type Pending =
  | { kind: "reset"; name: PromptName }
  | { kind: "delete-variant"; slot: PromptName; name: string }
  | { kind: "discard"; next: () => void };

export default function PromptsScreen() {
  const [prompts, setPrompts] = useState<Prompt[] | null>(null);
  const [summaries, setSummaries] = useState<Partial<Record<PromptName, PromptVariantSummary[]>>>({});
  const [loadError, setLoadError] = useState<string | null>(null);

  const [editor, setEditor] = useState<Editor | null>(null);
  const [draft, setDraft] = useState("");
  const [newName, setNewName] = useState("");
  const [newNameError, setNewNameError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  // busy names the variant an activate/delete is in flight for, per slot ("" is
  // the built-in default).
  const [busy, setBusy] = useState<{ slot: PromptName; name: string } | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [history, setHistory] = useState<{ slot: PromptName; name: string } | null>(null);

  const dirty =
    editor !== null &&
    (editor.card.mode === "new"
      ? draft !== editor.baseline || newName.trim() !== ""
      : draft !== editor.baseline);

  // Guard both exits from the page while an edit is unsaved.
  const { pendingHref, confirmNavigation, cancelNavigation } = useUnsavedGuard(dirty);

  // load fetches the slot views plus, for the generation slots, the variant
  // summaries the cards need for version tags (two extra parallel GETs).
  const load = useCallback(async () => {
    setLoadError(null);
    try {
      const [promptList, ...variantLists] = await Promise.all([
        listPrompts(),
        ...GENERATION_SLOTS.map((slot) => listVariants(slot)),
      ]);
      setPrompts(promptList);
      const bySlot: Partial<Record<PromptName, PromptVariantSummary[]>> = {};
      GENERATION_SLOTS.forEach((slot, i) => {
        bySlot[slot] = variantLists[i];
      });
      setSummaries(bySlot);
    } catch (e) {
      setPrompts(null);
      setLoadError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const closeEditor = () => {
    setEditor(null);
    setDraft("");
    setNewName("");
    setNewNameError(null);
  };

  // withDiscardGuard runs open() directly, or behind the discard confirm when
  // the open editor has unsaved changes.
  const withDiscardGuard = (open: () => void) => {
    if (dirty) {
      setPending({ kind: "discard", next: open });
      return;
    }
    open();
  };

  const effectiveValue = (slot: PromptName) =>
    prompts?.find((p) => p.name === slot)?.value ?? "";

  // openActiveEditor edits the slot's current selection (write-through save).
  const openActiveEditor = (slot: PromptName) =>
    withDiscardGuard(() => {
      setNotice(null);
      setNewName("");
      setNewNameError(null);
      const baseline = effectiveValue(slot);
      setEditor({ slot, card: { mode: "active" }, baseline });
      setDraft(baseline);
    });

  // openVariantEditor edits a named variant's head, fetched fresh so the draft
  // reflects what is stored (the card list only has summaries).
  const openVariantEditor = (slot: PromptName, name: string) =>
    withDiscardGuard(() => {
      setNotice(null);
      setNewName("");
      setNewNameError(null);
      void (async () => {
        try {
          const v = await getVariant(slot, name);
          setEditor({ slot, card: { mode: "variant", variantName: name }, baseline: v.value });
          setDraft(v.value);
        } catch (e) {
          setNotice({ slot, kind: "negative", message: errorMessage(e) });
        }
      })();
    });

  // openNewVariantEditor forks the current effective prompt into a new variant.
  const openNewVariantEditor = (slot: PromptName) =>
    withDiscardGuard(() => {
      setNotice(null);
      setNewName("");
      setNewNameError(null);
      const baseline = effectiveValue(slot);
      setEditor({ slot, card: { mode: "new" }, baseline });
      setDraft(baseline);
    });

  // replace swaps one prompt in the list with the daemon's updated view, so the
  // chip, radio selection, and preview reflect what the daemon now holds.
  const replace = (updated: Prompt) => {
    setPrompts((list) => list?.map((p) => (p.name === updated.name ? updated : p)) ?? [updated]);
  };

  const onSave = async () => {
    if (!editor) return;
    const { slot, card } = editor;
    setSaving(true);
    setNotice(null);
    try {
      switch (card.mode) {
        case "active": {
          replace(await savePrompt(slot, draft));
          await refreshSummaries(slot);
          setNotice({ slot, kind: "positive", message: SAVED_MESSAGE });
          break;
        }
        case "variant": {
          const v = await saveVariant(slot, card.variantName, draft);
          await load();
          setNotice({
            slot,
            kind: "positive",
            message: v.active
              ? `Saved “${card.variantName}” (v${v.version}). New chats and batch runs will use it.`
              : `Saved “${card.variantName}” (v${v.version}). The active prompt is unchanged.`,
          });
          break;
        }
        case "new": {
          const name = newName.trim();
          if (!VARIANT_NAME_PATTERN.test(name)) {
            setNewNameError("Use lowercase letters, digits and hyphens (max 64 characters).");
            setSaving(false);
            return;
          }
          if (name === "default") {
            setNewNameError('"default" is reserved — choose another name.');
            setSaving(false);
            return;
          }
          await createVariant(slot, name, draft);
          await load();
          setNotice({
            slot,
            kind: "positive",
            message: `Variant “${name}” created. Select it above to use it in new chats and batch runs.`,
          });
          break;
        }
      }
      closeEditor();
    } catch (e) {
      // Keep the editor open and the draft intact: a failed save must never
      // cost the user their text. A create failure (name clash) stays on the
      // name field.
      if (card.mode === "new") {
        setNewNameError(errorMessage(e));
      } else {
        setNotice({ slot, kind: "negative", message: errorMessage(e) });
      }
    } finally {
      setSaving(false);
    }
  };

  // refreshSummaries re-fetches one slot's variant summaries (e.g. after a
  // write-through save bumped a version or created `custom`).
  const refreshSummaries = async (slot: PromptName) => {
    if (!isGenerationSlot(slot)) return;
    try {
      const list = await listVariants(slot);
      setSummaries((s) => ({ ...s, [slot]: list }));
    } catch {
      // Version tags going momentarily stale is not worth an error banner.
    }
  };

  const onActivate = async (slot: PromptName, variant: string) => {
    setBusy({ slot, name: variant });
    setNotice(null);
    try {
      replace(await activatePrompt(slot, variant));
      setNotice({
        slot,
        kind: "positive",
        message: variant
          ? `Now using “${variant}”. New chats and batch runs will use it.`
          : "Back to the built-in default. New chats and batch runs will use it.",
      });
    } catch (e) {
      setNotice({ slot, kind: "negative", message: errorMessage(e) });
    } finally {
      setBusy(null);
    }
  };

  const onDeleteVariant = async (slot: PromptName, name: string) => {
    setBusy({ slot, name });
    setNotice(null);
    try {
      await deleteVariant(slot, name);
      await load();
      setNotice({ slot, kind: "positive", message: `Variant “${name}” deleted.` });
    } catch (e) {
      setNotice({ slot, kind: "negative", message: errorMessage(e) });
    } finally {
      setBusy(null);
      setPending(null);
    }
  };

  const onReset = async (name: PromptName) => {
    setSaving(true);
    setNotice(null);
    try {
      replace(await resetPrompt(name));
      closeEditor();
      setNotice({
        slot: name,
        kind: "positive",
        message: "Prompt reset to its default. New chats and batch runs will use it.",
      });
    } catch (e) {
      setNotice({ slot: name, kind: "negative", message: errorMessage(e) });
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
    if (pending.kind === "delete-variant") {
      void onDeleteVariant(pending.slot, pending.name);
      return;
    }
    // Discarding unsaved edits, then continuing where the user was headed.
    closeEditor();
    const next = pending.next;
    setPending(null);
    next();
  };

  // onRestored refreshes the effective view after a restore (the restored
  // variant may be the active one) and closes the history modal.
  const onRestored = () => {
    const slot = history?.slot;
    void load();
    setHistory(null);
    if (slot) {
      setNotice({ slot, kind: "positive", message: "Version restored as the latest." });
    }
  };

  return (
    <>
      <Header title="Prompts" />
      <main className="app-main prompts">
        <p className="prompts__intro u-text--muted">
          These templates drive the assistant. Saved prompts are held by the daemon and used by
          chat sessions, batch runs, and the <code>rag-cli.rag prompt</code> commands.
        </p>

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
            variants={summaries[prompt.name] ?? []}
            title={PROMPT_LABELS[prompt.name].title}
            description={PROMPT_LABELS[prompt.name].description}
            isGeneration={isGenerationSlot(prompt.name)}
            notice={notice?.slot === prompt.name ? notice : null}
            editor={editor?.slot === prompt.name ? editor.card : null}
            draft={editor?.slot === prompt.name ? draft : ""}
            dirty={editor?.slot === prompt.name && dirty}
            saving={saving && editor?.slot === prompt.name}
            onDraftChange={setDraft}
            onSave={() => void onSave()}
            onCancel={() => withDiscardGuard(closeEditor)}
            onReset={() => setPending({ kind: "reset", name: prompt.name })}
            newName={editor?.slot === prompt.name ? newName : ""}
            newNameError={editor?.slot === prompt.name ? newNameError : null}
            onNewNameChange={(value) => {
              setNewName(value);
              setNewNameError(null);
            }}
            onEdit={() => openActiveEditor(prompt.name)}
            onStartNewVariant={() => openNewVariantEditor(prompt.name)}
            busyName={busy?.slot === prompt.name ? busy.name : null}
            onActivate={(variant) => void onActivate(prompt.name, variant)}
            onEditVariant={(name) => openVariantEditor(prompt.name, name)}
            onOpenHistory={(name) => setHistory({ slot: prompt.name, name })}
            onDeleteVariant={(name) => setPending({ kind: "delete-variant", slot: prompt.name, name })}
          />
        ))}
      </main>

      {history && (
        <VariantHistoryModal
          slot={history.slot}
          name={history.name}
          onRestored={onRestored}
          onClose={() => setHistory(null)}
        />
      )}

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
            Returns <strong>{PROMPT_LABELS[pending.name].title.toLowerCase()}</strong>{" "}
            to the built-in default. Any stored variants are kept. New chats and batch runs will use the default.
          </p>
        </ConfirmModal>
      )}

      {pending?.kind === "delete-variant" && (
        <ConfirmModal
          title="Delete variant"
          confirmLabel="Delete variant"
          destructive
          busy={busy !== null}
          onConfirm={onConfirmPending}
          onClose={() => setPending(null)}
        >
          <p>
            Deletes the variant <strong>{pending.name}</strong> and its version history. This cannot be undone.
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
