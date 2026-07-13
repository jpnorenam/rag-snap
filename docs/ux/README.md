# UX guidelines for the UI↔CLI parity hackathon

One document per OpenSpec change in `PLAN.md`, plus a shared foundation. These are written to be consumed by a coding agent during `/opsx:apply`: they are prescriptive about layout, component choice, states, and accessibility, and they cite the conventions they derive from.

## How to use these docs

- **When proposing** (`/opsx:propose`): link the change's UX doc from `design.md` and copy its "Definition of done (UX)" checklist into `tasks.md` as a task.
- **When applying** (`/opsx:apply`): read `00-foundation.md` **first**, then the change's own doc. The foundation doc wins over generic Vanilla examples found online; the change doc wins over the foundation doc only where it explicitly says so.
- Never introduce a visual pattern that isn't in the foundation doc without adding it there in the same PR.

## Index

| Doc | Change | Scope |
|---|---|---|
| [00-foundation.md](00-foundation.md) | all | Theme, tokens, layout shell, naming, component conventions, states, a11y |
| [01-app-shell.md](01-app-shell.md) | `add-ui-app-shell` | Routing/nav, global operations indicator |
| [02-knowledge-management.md](02-knowledge-management.md) | `add-ui-knowledge-management` | KB list/detail, sources, ingest, engine init, export/import |
| [03-search.md](03-search.md) | `add-ui-search` | Search page, result cards |
| [04-answer-batch.md](04-answer-batch.md) | `add-ui-answer-batch` | Manifest compose/run, results review, RFP build wizard |
| [05-prompts.md](05-prompts.md) | `add-prompts-api-and-ui` | Prompt editor page |
| [06-status-settings.md](06-status-settings.md) | `add-ui-status-settings` | Status page, config browser/editor |
| [07-gdrive-import.md](07-gdrive-import.md) | `add-gdrive-import-api-ui` | Drive OAuth + archive picker |

## Sources of truth (in precedence order)

1. **Existing `ui/` code** — `ui/app/globals.scss`, `ui/components/*.tsx`, `ui/lib/api/*`. When in doubt, imitate what ships.
2. **Vanilla Framework 4.51** (`node_modules/vanilla-framework`) — the visual system. Before using any `p-*`/`u-*` class not already used in `ui/`, verify it exists in the installed version (`grep -r "p-modal" node_modules/vanilla-framework/scss/` or the docs at vanilla-framework.io for v4).
3. **`../web-code-standards`** — Canonical's code standards. It governs the design-system component library (`.ds` namespace), **not** Vanilla consumers, so its CSS namespace rules do *not* apply here. We adopt its portable rules: React component structure & props (`docs/react.md`), className construction, semantic (not presentational) class names, design-token discipline (`docs/styling.md`, mapped onto Vanilla's `--vf-*` custom properties), TypeScript rules (`docs/code.md`), and ARIA/keyboard/focus requirements (`docs/webcomponents.md`).
