# 00 — Foundation: theme, layout, and interaction conventions

Applies to **every** change. Change-specific docs assume all of this.

## 1. Theme & tokens

- Vanilla Framework is imported **whole** in `ui/app/globals.scss` (`@use "vanilla-framework"; @include vanilla-framework.vanilla;`). Do not switch to selective includes mid-hackathon.
- **Dark mode** is Vanilla's `is-dark` class on `<html>`, toggled by `ui/lib/useDarkMode.ts` (persisted in `localStorage["darkMode"]`). Every new screen must look correct in both themes — test both before marking a task done.
- **Color rule** (token discipline per `web-code-standards` `cs:css.properties.values`, mapped to Vanilla): any color that must respond to theme comes from a Vanilla custom property. Never hardcode hex values in component styles. Sanctioned tokens already in use:
  - Surfaces: `--vf-color-background-alt`, `--vf-color-background-information-default`, `--vf-color-background-caution-default`
  - Borders: `--vf-color-border-default`, `--vf-color-border-positive`, `--vf-color-border-negative`, `--vf-color-border-caution`
  - Text: `--vf-color-text-muted` (or the `u-text--muted` utility)
- The **only** hardcoded colors allowed are the sidebar rail's, already defined as SCSS vars in `globals.scss`: `$sidebar-bg: #262626` (charcoal) and `$sidebar-accent: #e95420` (Ubuntu orange). The orange is a brand accent for the rail, logo, and active-nav indicator only — never use it for content-area semantics (use positive/negative/caution tokens there).
- Spacing/typography: `rem` units, lean on Vanilla defaults. Uppercase micro-labels use the existing recipe: `0.75–0.875rem`, `text-transform: uppercase; letter-spacing: 0.05em`, muted color.

## 2. Layout shell

The app shell is a flex row (`.app-shell`): dark `<Sidebar>` rail (15rem, sticky, full height) + `.app-content` column = `<Header>` (`.app-topbar`, shows the section title and a status slot) + `<main className="app-main">`. Content is capped at `64rem` and centered. At `max-width: 620px` the rail collapses to a 3.5rem icon rail.

New screens plug in as: sidebar entry → route → `<Header title="…">` + `<main>` content. Do not invent alternative shells, full-bleed layouts, or second sidebars. Wide tables scroll inside their own `overflow-x: auto` wrapper; the page never scrolls horizontally.

## 3. CSS class naming

- Custom classes: **feature-prefixed BEM** exactly as the codebase does it — `.kb-list`, `.kb-list__row`, `.kb-list__row--selected`; app-level pieces use `.app-*`. Block `__element` for children, `--modifier` for variants.
- State classes: Vanilla's `is-*` convention (`.is-active`, `.is-connected`, `.is-error`), and prefer native attribute states where they exist (`[disabled]`, `[aria-current="page"]`) per `web-code-standards` `cs:css.component.states`.
- Names are **semantic, not presentational** (`cs:css.selectors.semantics`): `.source-row--failed`, not `.source-row--red`.
- All custom styles live in `ui/app/globals.scss` (one flat file, grouped by feature with a `// --- feature ---` comment header). No CSS modules, no styled-components, no inline style objects except truly dynamic values (e.g. a progress-bar width percentage).

## 4. React component conventions

- No component library — **hand-write Vanilla class names** in JSX. Do not add `@canonical/react-components` or any UI dependency.
- Components: `ui/components/`, `"use client"`, PascalCase default export, typed `Props` interface, `@/` import alias. Shared primitives introduced by these changes go in `ui/components/common/` (per `web-code-standards` `cs:react.component.dependencies`).
- className construction for components with variants (adopted from `cs:react.component.class_name_construction`): build from an array ordered base → modifiers → consumer `className`, then `.filter(Boolean).join(" ")`. No template-literal soup.
- State: local `useState`/`useRef`/`useCallback` only; `useRef` for values read inside async callbacks (see `connRef`/`awaitingDone` in `ChatScreen.tsx`). Introduce React Context only for the global operations tracker (Change 1) — nothing else.
- Extract a custom hook only for a real single concern (`cs:react.hooks.custom`); name it `use*`; put shared ones in `ui/lib/`.
- Navigation between screens uses `next/link` (`<Link href="/knowledge/">`). Remember `basePath: '/ui'` + `trailingSlash: true` + static export: pages are `ui/app/<route>/page.tsx`, client-side only, no server components/actions/route handlers/`next/image`.

## 5. API interaction

- All calls go through `ui/lib/api/envelope.ts`: `getSync` / `postSync` / `postAsync` (+ add `deleteSync`/`putSync` siblings following the same `request()` pattern when a change needs them). Never call `fetch` directly from a component.
- Paths are runtime-relative via `apiUrl()`; never bake a host/port.
- `ApiError.code === 0` means the daemon/backend is unreachable — show the standard connection error (§7), not the raw message. Other codes: show `error.message` in a negative notification.
- One feature module per resource in `ui/lib/api/` (`knowledge.ts`, `chat.ts`, …) exporting typed interfaces that mirror daemon views plus thin verb functions; normalize `null` arrays to `[]`.
- Long-running work is an **operation**: `postAsync`, then track via the Change-1 operations context (`useOperations`). Never hand-roll polling loops inside screens.

## 6. Canonical component vocabulary (the sanctioned set)

Use these Vanilla patterns for these jobs — and nothing else without adding it here:

| Job | Pattern |
|---|---|
| Primary action | `p-button--positive` (one per view, max) |
| Secondary/neutral action | `p-button` |
| Destructive action | `p-button--negative` (only inside a confirm flow, §8) |
| Quiet/inline action | `p-button--base` |
| Toggle chips (multi-select) | `p-chip` / `p-chip--positive` + `p-chip__value` (KB selector pattern in `ChatScreen.tsx`) |
| Error banner | `p-notification--negative` + `role="alert"` (exact markup from `ChatScreen.tsx`) |
| Success / info / warning banner | `p-notification--positive` / `--information` / `--caution`, same structure |
| Data lists | semantic `<table>` (Vanilla styles it) with `<caption class="u-off-screen">` or `aria-label`; numeric columns `u-align--right`; row actions right-aligned `p-button--base` |
| Forms | `p-form p-form--stacked`, `p-form__group` + `<label>`; validation via `p-form-validation is-error` + `p-form-validation__message` |
| Modals | `p-modal` overlay + `p-modal__dialog`, `role="dialog"` `aria-modal="true"` `aria-labelledby` on the title; close on Escape and overlay click |
| Inline code / IDs / snippets | `p-code-snippet` with `p-code-snippet__block` (copyable where useful) |
| Muted/secondary text | `u-text--muted`, `p-text--small` |
| Status dot | the existing custom `.chat__status-dot` + `is-*` recipe — generalize to `.app-status-dot` in Change 1 |
| Spinner | `<i className="p-icon--spinner u-animation--spin" aria-hidden="true" />` + visible text ("Loading…") |
| Icons | inline line-SVG via the `NavIcon` pattern (`stroke="currentColor"`, 20×20, `viewBox 0 0 24 24`, `aria-hidden`); extend the `IconName` union, don't add an icon library |

Verify any Vanilla class not listed above exists in the installed `vanilla-framework@4.51` before using it (`grep -r "class-name" node_modules/vanilla-framework/scss/`).

## 7. Standard view states (every data screen implements all four)

1. **Loading** — spinner + text, centered in the content area. No layout shift when data lands (reserve table/header space where cheap).
2. **Empty** — the app empty-state pattern (define once in Change 1 as `components/common/EmptyState.tsx`): icon (muted), one-line headline stating what's missing, one sentence of guidance including the CLI equivalent, and the primary action. Example: "No knowledge bases yet. Create one here or run `rag-cli.rag k create <name>`." Empty ≠ error.
3. **Loaded** — the content.
4. **Error** — `p-notification--negative` with a human sentence + a **Retry** action where retry makes sense. For `ApiError.code === 0`: "Cannot reach the RAG daemon. Check that the service is running (`snap services rag-cli`)."

Mutations additionally get: in-flight = the triggering button disabled with spinner icon + label change ("Creating…"); success = positive notification or optimistic list update; failure = negative notification **without** losing the user's input.

## 8. Destructive actions

Match CLI semantics. Deleting a KB uses a **type-to-confirm modal**: body states consequences ("Deletes the index and all N ingested sources. This cannot be undone."), a text input where the user must type the KB name, and a `p-button--negative` that stays `[disabled]` until the input matches exactly. Lesser deletions (forget a source, cancel an operation) use a plain confirm modal with the object named in the body. Never `window.confirm`.

## 9. Accessibility (from `web-code-standards` `docs/webcomponents.md`, applied to plain React)

- Every interactive element is a native `<button>`/`<a>`/`<input>` — never a clickable `<div>`. Icon-only controls get an explicit `aria-label`.
- Keyboard per WAI-ARIA: Enter/Space activate; Escape closes modals/panels; arrow keys within composite widgets (tabs, chip groups). No click-only handlers.
- Modals trap focus, move focus to the dialog on open, and restore it on close.
- Route changes update the `<Header>` title and `document.title`.
- Live updates: streaming/progress regions use `aria-live="polite"`; errors use `role="alert"`. Decorative SVGs are `aria-hidden`.
- Active nav uses `aria-current="page"`; disabled nav placeholders stay non-focusable `<span>`s (per `cs:react.component.link_component`: non-navigable items are never links or buttons).

## 10. Microcopy

- Sentence case everywhere (headings, buttons, labels): "Create knowledge base", not "Create Knowledge Base".
- Buttons are verb-first and specific ("Ingest document", "Run batch"), never "OK"/"Submit".
- Errors say what happened and what to do next, in one or two sentences. Include the CLI fallback command when one exists — the CLI is the power-user escape hatch and part of the parity story.
- Timestamps: relative in lists ("3 min ago") with the absolute time in `title`.

## Definition of done (UX) — shared checklist

Copy into each change's `tasks.md`:

- [ ] All four view states implemented per foundation §7; mutations follow §7's in-flight/success/failure rules
- [ ] Looks correct in light **and** dark themes (`is-dark`)
- [ ] Usable at 620px (collapsed rail) — no horizontal page scroll
- [ ] Keyboard-only walkthrough passes (§9); focus management on modals/routes verified
- [ ] Only sanctioned patterns (§6) used; any new pattern added to `docs/ux/00-foundation.md`
- [ ] All colors via `--vf-*` tokens; zero hardcoded hex outside the sidebar SCSS vars
- [ ] Empty states include the CLI-equivalent command
