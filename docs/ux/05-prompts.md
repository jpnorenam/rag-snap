# 05 — UX guidelines: `add-prompts-api-and-ui`

`/prompts/` — view and edit the three prompt templates. Parity with `prompt init` (a `huh` select + multiline editor in the CLI). Read `00-foundation.md` first.

## Layout

One page, three stacked **prompt cards** in fixed order (matching the CLI's select order):

1. `chat_system_prompt` — "Chat system prompt" · "Sets the assistant's behavior in interactive chat."
2. `answer_system_prompt` — "Answer system prompt" · "Used for batch answering (RFPs)."
3. `source_rules` — "Source rules" · "Rules for how retrieved sources are cited and used."

Card anatomy (`.prompt-card`, standard surface + border tokens):
- Header: title (H2) + a state chip: **Default** (plain `p-chip`) or **Customized** (`p-chip--positive`).
- Collapsed body: the first ~4 lines of the *effective* prompt in a read-only `p-code-snippet__block` with a fade-out, + **Edit** (`p-button`).
- Expanded (edit mode): a monospace `<textarea>` (min-height ~16 lines, autogrows; label wired via `<label>`), footer actions: **Save** (`p-button--positive`), **Cancel** (`p-button`), and — only when customized — **Reset to default** (`p-button--negative` styling is too strong here; use `p-button--base` with confirm modal: "Replaces your customized prompt with the built-in default.").

Only one card is in edit mode at a time; entering edit on another card with unsaved changes prompts a confirm.

## Behavior & honesty about scope

- Show the **default text** accessibly: in edit mode, a `<details>` "View default prompt" under the textarea renders the built-in default in a read-only snippet, so users can diff by eye and copy from it.
- Dirty tracking: Save disabled until content differs from the stored value; `beforeunload` + in-app confirm on navigation with unsaved edits.
- After save: positive notification "Prompt saved. New chats and batch runs will use it." — this sentence is load-bearing: it tells the user the change applies to *future* sessions/operations, matching Story 5.2 semantics. If the daemon applies prompts mid-session, adjust the copy accordingly; never promise what the backend doesn't do.
- **Migration note**: if the CLI-local `~/.config/rag-cli/prompts.json` exists but daemon-side prompts are default (the API should surface this), show a one-time `p-notification--information`: "You have CLI prompt customizations that the daemon doesn't use yet. Re-save them here to apply everywhere." Dismissible; don't auto-migrate silently.

## States
- Loading: three skeleton cards (fixed height, no shift).
- Error: foundation §7; editing is blocked while prompts can't load.
- There is no "empty" — defaults always exist.
- Save failure: keep the textarea content, negative notification with retry.

## Accessibility
- Cards are a list of `<section>`s with `aria-labelledby` their H2.
- Textarea keeps standard textarea semantics (no fancy editor, no syntax highlighting library).
- The Default/Customized chip has a text label, not color alone.
- Escape in edit mode = Cancel (with dirty confirm).

## Definition of done (UX)
Foundation checklist, plus:
- [ ] Default vs customized state visible per prompt; default text viewable and copyable while editing
- [ ] Reset-to-default flows through a confirm modal and restores the shown default exactly
- [ ] Unsaved-changes guards on card switch and navigation
- [ ] Post-save copy accurately reflects when the prompt takes effect
- [ ] Monospace editing area usable in both themes (verify textarea colors come from tokens)
