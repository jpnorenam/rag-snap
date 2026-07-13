# 03 — UX guidelines: `add-ui-search`

`/search/` — retrieval-only inspection. Parity with `k search` and the in-chat `/search` slash command. Small, self-contained. Read `00-foundation.md` first.

## Layout

Single column under the shell, content capped at the standard `64rem`:

1. **Query bar** — Vanilla `p-search-box` (input + submit button, `aria-label="Search knowledge bases"`). Enter submits. The query persists in the URL (`/search/?q=…`) so results are shareable/reloadable.
2. **Scope row** directly under the bar:
   - KB multi-select as **chips** — reuse the exact `p-chip`/`p-chip--positive` toggle pattern from `ChatScreen.tsx`. Default: all bases selected if only one exists, otherwise the `default` base (mirrors `k search -b default`).
   - **Top-k** — a compact `<select>` labeled "Results": 5 / 10 / 15 / 25, default **10** (parity with `k search --top`; the chat `/search` default of 15 is available as an option).
3. **Results list**.

No sidebar filters, no pagination — top-k *is* the result budget, same as the CLI.

## Result cards

One card per chunk (`.search-result`, surface `--vf-color-background-alt`, standard border), ranked order:

- **Header line**: rank number (muted) · source title or source ID (strong) · KB name as a small `p-chip` (non-interactive `<span>` chip) · **score** right-aligned, `p-text--small u-text--muted`, 3 decimals (parity: CLI prints score/provenance).
- **Body**: the chunk text, preserving paragraph breaks (`white-space: pre-wrap`), full text — do not truncate; this screen exists to inspect retrieval, same as `/search` in the REPL prints full chunks.
- **Footer**: provenance details (source ID, any URL) in `p-text--small`; **View source metadata** as `p-button--base` → links to the Change-2 metadata view (`/knowledge/?kb=<name>&source=<id>`). If Change 2 hasn't landed, render the source ID as plain text — feature-detect by route existence at build time, don't 404.

## States

- **Initial (no query yet)**: `EmptyState` — "Search your knowledge bases." + one line explaining it's hybrid semantic + lexical retrieval with reranking, no LLM involved + CLI hint `rag-cli.rag k search "<query>"`.
- **Loading**: spinner replaces the results area; query bar stays interactive-disabled (button shows spinner) to prevent double-submit.
- **No hits**: "No matching chunks in `<bases>`." + suggestion to widen the base selection or raise top-k.
- **No KBs exist at all**: caution notification linking to `/knowledge/` ("Create and ingest a knowledge base first").
- **Error**: foundation §7.

## Accessibility & keyboard

- Results region is `aria-live="polite"` announced as "N results".
- Focus stays in the search input after submit; results start with an `<h2 class="u-off-screen">Results</h2>` for AT navigation.
- Chips and the select are reachable in tab order between the input and results.

## Definition of done (UX)
Foundation checklist, plus:
- [ ] Query + scope round-trip through the URL; reload reproduces the search
- [ ] Chips reuse the ChatScreen pattern verbatim (light + dark verified)
- [ ] Full chunk text rendered, scores and provenance visible — output matches what `k search` prints for the same query
- [ ] No-hits vs error vs no-KBs are three distinct states
