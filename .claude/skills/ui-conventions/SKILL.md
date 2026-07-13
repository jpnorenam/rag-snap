---
name: ui-conventions
description: Canonical web theme and code conventions for this repo's browser UI. Use when creating or modifying anything under ui/ — React components, Next.js pages/routes, globals.scss, Vanilla Framework markup, dark mode, or the ui/lib/api client — or when reviewing UI changes.
---

# UI conventions (Canonical Vanilla theme)

The UI in `ui/` is a Next.js App Router SPA, statically exported and embedded into the `ragd` daemon (`go:embed` via `internal/webui/`), served same-origin with the REST API under `/ui/`. It uses **Vanilla Framework 4** as plain CSS classes — there is **no component library** (no `@canonical/react-components`); Vanilla markup is hand-written in JSX. Do not add UI dependencies.

## Sources of truth, in precedence order

1. **Existing `ui/` code** — imitate what ships (`ui/app/globals.scss`, `ui/components/*.tsx`, `ui/lib/api/*`).
2. **Installed Vanilla Framework** — before using any `p-*`/`u-*` class not already present in `ui/`, verify it exists: `grep -r "<class-name>" ui/node_modules/vanilla-framework/scss/`.
3. **`../web-code-standards`** (sibling repo) — Canonical code standards. **Caveat:** it governs the design-system component library (`.ds` CSS namespace, token architecture); it is *not* a Vanilla usage guide and never mentions `p-*` classes. Apply only its portable rules: React component structure/props and className construction (`docs/react.md`), semantic-not-presentational class names and attribute-based states (`docs/css.md`), TypeScript rules (`docs/code.md`), ARIA/keyboard/focus (`docs/webcomponents.md`). Never apply its `.ds` namespace rules here.

## Theme & color

- Dark mode = Vanilla's `is-dark` class on `<html>`, toggled by `ui/lib/useDarkMode.ts` (persisted in `localStorage["darkMode"]`). Every screen must be verified in **both** themes.
- Any color that responds to theme comes from a Vanilla custom property (`--vf-*`): surfaces `--vf-color-background-alt`, borders `--vf-color-border-{default,positive,negative,caution}`, text `--vf-color-text-muted`, info/caution backgrounds `--vf-color-background-{information,caution}-default`. **Never hardcode hex** in component styles.
- Sole exceptions, already defined as SCSS vars in `globals.scss`: the dark sidebar rail's `$sidebar-bg: #262626` and `$sidebar-accent: #e95420` (Ubuntu orange). The orange is brand accent for the rail/logo/active-nav only — content-area semantics use positive/negative/caution tokens.
- Spacing/typography: `rem`, lean on Vanilla defaults. Uppercase micro-labels: `0.75–0.875rem`, `text-transform: uppercase; letter-spacing: 0.05em`, muted.

## Layout shell

Flex row `.app-shell` = dark `<Sidebar>` rail (15rem, sticky) + `.app-content` column = `<Header>` (`.app-topbar`, section title + status slot) + `<main className="app-main">`. Content capped at `64rem`, centered. At `max-width: 620px` the rail collapses to a 3.5rem icon rail. New screens plug into this shell — no alternative shells, full-bleed layouts, or second sidebars. Wide tables scroll inside their own `overflow-x: auto` wrapper; the page never scrolls horizontally.

## CSS conventions

- Custom classes: feature-prefixed BEM — `.kb-list`, `.kb-list__row`, `.kb-list__row--selected`; app-level pieces `.app-*`. States use Vanilla's `is-*` (`.is-active`, `.is-error`) and native attributes where they exist (`[disabled]`, `[aria-current="page"]`).
- Names are semantic, not presentational (`.source-row--failed`, never `--red`).
- All custom styles go in `ui/app/globals.scss`, grouped per feature under a `// --- feature ---` comment. No CSS modules, no styled-components, no inline styles except truly dynamic values (e.g. progress width).

## React conventions

- Components: `ui/components/` (shared primitives in `ui/components/common/`), `"use client"`, PascalCase default export, typed `Props` interface, `@/` import alias.
- Reuse before creating: check `ui/components/common/` for existing primitives (empty state, confirm modal, spinner, operations tracking) before writing new ones.
- className with variants: build from an array ordered base → modifiers → consumer `className`, then `.filter(Boolean).join(" ")` — no template-literal soup.
- State: local `useState`/`useRef`/`useCallback`; `useRef` for values read inside async callbacks (see `connRef`/`awaitingDone` in `ChatScreen.tsx`). No Redux/Zustand; React Context only for genuinely app-global concerns.
- Static-export constraints (`next.config.js`: `output: 'export'`, `basePath: '/ui'`, `trailingSlash: true`): pages are `ui/app/<route>/page.tsx`, links are `/route/` via `next/link`; no server components, server actions, route handlers, `next/image` loader, or dynamic path segments (use query params + `useSearchParams()` for detail views).
- Icons: inline line-SVG via the `NavIcon` pattern in `Sidebar.tsx` (`stroke="currentColor"`, 20×20, `viewBox 0 0 24 24`, `aria-hidden`). Extend the `IconName` union; never add an icon library.

## API client conventions (`ui/lib/api/`)

- All requests go through `envelope.ts` (`getSync`/`postSync`/`postAsync`, LXD-style sync/async/error envelope); add sibling verbs following the same `request()` pattern when needed. Never `fetch` directly from a component.
- Paths are runtime-relative via `apiUrl()`; never bake a host/port. Auth is the loopback cookie + `authHeaders()` fallback — call `captureTokenFromUrl()` once on mount before any API call.
- `ApiError.code === 0` = daemon unreachable → show the standard connection message ("Cannot reach the RAG daemon. Check that the service is running (`snap services rag-cli`)."), not the raw error.
- One feature module per resource exporting typed interfaces mirroring daemon views + thin verb functions; normalize `null` arrays to `[]`.
- Long-running work is a daemon **operation**: `postAsync`, then track it through the shared operations mechanism if one exists in `ui/components/common/` — don't hand-roll polling loops inside screens.

## Sanctioned component vocabulary

| Job | Pattern |
|---|---|
| Primary action | `p-button--positive` (max one per view) |
| Secondary / quiet | `p-button` / `p-button--base` |
| Destructive | `p-button--negative`, only inside a confirm flow |
| Toggle chips | `p-chip` / `p-chip--positive` + `p-chip__value` (see KB selector in `ChatScreen.tsx`) |
| Banners | `p-notification--negative` (+`role="alert"`) / `--positive` / `--information` / `--caution`, structure as in `ChatScreen.tsx` |
| Data lists | semantic `<table>` with `aria-label` or off-screen caption; numeric cols `u-align--right` |
| Forms | `p-form p-form--stacked`, `p-form__group` + `<label>`; errors via `p-form-validation is-error` + `__message` |
| Modals | `p-modal` + `p-modal__dialog`, `role="dialog" aria-modal="true" aria-labelledby`; Escape + overlay close; focus trapped, restored on close |
| Code / IDs | `p-code-snippet` + `p-code-snippet__block` |
| Spinner | `<i className="p-icon--spinner u-animation--spin" aria-hidden="true" />` + visible text |
| Muted text | `u-text--muted`, `p-text--small` |

New visual patterns require updating this skill in the same PR.

## View states & destructive actions

Every data screen implements four states: **loading** (spinner + text, no layout shift), **empty** (icon + one-line headline + guidance including the CLI-equivalent command + primary action — empty ≠ error), **loaded**, **error** (negative notification + Retry where sensible). Mutations: trigger button disabled with spinner + label change in flight; failure never loses user input.

Destructive actions never use `window.confirm`. Deleting a knowledge base = type-to-confirm modal (state consequences, require typing the exact name, `p-button--negative` disabled until match). Lesser deletions = plain confirm modal naming the object.

## Accessibility

- Interactive elements are native `<button>`/`<a>`/`<input>` — never clickable `<div>`s. Icon-only controls get `aria-label`.
- Keyboard per WAI-ARIA: Enter/Space activate, Escape dismisses, arrows within composite widgets. No click-only handlers.
- Route changes update the `<Header>` title and `document.title`. Active nav = `aria-current="page"`; non-navigable placeholders are `<span>`s, never links/buttons.
- Streaming/progress regions `aria-live="polite"`; errors `role="alert"`; decorative SVGs `aria-hidden`.

## Microcopy

Sentence case everywhere. Buttons are verb-first and specific ("Ingest document", not "OK"). Errors: what happened + what to do next, one or two sentences, with the CLI fallback command when one exists. Timestamps relative in lists, absolute in `title`.

## Done means

Both themes verified · usable at 620px with no horizontal page scroll · keyboard-only walkthrough passes · all colors via `--vf-*` tokens · only sanctioned patterns (or this skill updated) · all four view states present.
