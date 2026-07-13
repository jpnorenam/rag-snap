# 07 — UX guidelines: `add-gdrive-import-api-ui`

Google Drive import inside the Change-2 import flow. Parity with `k import --url <drive-url>` (interactive picker / `--all`). Stretch change — read `00-foundation.md`, `01-app-shell.md`, and `02-knowledge-management.md` first.

## Where it lives

**Not a new page.** The Change-2 import modal on `/knowledge/` gains a second source option: **From file** | **From Google Drive** (same tab/radio pattern as the ingest modal). Remove the "use the CLI for Drive" hint line added in Change 2.

## Flow

### Step 1 — Connect
- If the daemon has no valid Drive token: a card with a Google line-icon (add to `NavIcon` union, `aria-hidden`), one sentence ("Connect a Google account to import knowledge-base archives from Drive."), and **Connect Google Drive** (`p-button--positive`).
- Clicking starts the daemon-side OAuth flow. The UI opens Google's consent URL in a **new tab** (never navigate the app away — SPA state would be lost) and shows a waiting state in the modal: spinner + "Waiting for Google authorization… Complete the consent screen in the new tab." + **Cancel**.
- The modal polls/subscribes for auth completion; on success it advances automatically. On denial/timeout: negative notification inside the modal, with retry.
- Once connected, show the connected account (email if the API exposes it) + a quiet **Disconnect** (`p-button--base`, confirm modal — it deletes the stored token).

### Step 2 — Locate
- One field: **Drive folder or file URL** (`p-form--stacked`), same URL forms the CLI accepts (documented in `docs/usage.md`). Validate shape client-side; resolve via the API on submit.
- Resolution errors are specific: not found / no access with this account / not a rag-cli archive — each with one actionable sentence.

### Step 3 — Pick archives (parity with the CLI's `huh` multi-select and `--all`)
- If the URL is a single archive file: skip picking, go to confirm.
- If a folder: a checkbox list of discovered archives — name · size · modified (relative). **Select all** checkbox at the top (this *is* `--all`). Selected count in the footer.
- **Force** checkbox (shared semantics with Change-2 import: overwrite existing KBs; caution-styled helper text listing which selected names collide, if the API can report it).

### Step 4 — Import
- **Import N archives** (`p-button--positive`) → one tracked operation per archive (or one batch operation — follow the API design), close the modal, completion via the operations panel; KB list refreshes as imports land. Partial failure is per-archive in the ops panel, not all-or-nothing messaging.

## Trust & privacy copy
- Before first connect, one muted sentence: "The authorization token is stored by the daemon on this machine and used only to read the archives you select." Keep it accurate to the token-storage design decision — update the copy if storage differs.
- Never render the token, refresh token, or raw OAuth URLs with embedded secrets in the UI or the operations panel descriptions.

## States
Foundation §7 plus flow-specific ones above. If loopback auth is fine but Drive endpoints report the feature disabled/unconfigured (`gdrive.client.id` unset), the Drive tab renders an information state: "Google Drive import isn't configured. Set `gdrive.client.id` and `gdrive.client.secret` (package config)." — with the Status page linked.

## Accessibility
- The multi-step modal announces step changes (`aria-live="polite"`); each step has a heading.
- The archive list is a real checkbox group (fieldset + legend); Select all is a tri-state checkbox (`indeterminate` when partially selected).
- The consent-tab handoff must be reachable keyboard-only; the waiting state's Cancel is focusable immediately.

## Definition of done (UX)
Foundation checklist, plus:
- [ ] OAuth handoff opens in a new tab; app state survives; denial and timeout produce distinct recoverable states
- [ ] Folder picking supports select-all (parity `--all`) and per-archive selection; single-file URLs skip the picker
- [ ] Force semantics identical to Change-2 local import
- [ ] Per-archive success/failure visible in the operations panel; partial success handled
- [ ] Disconnect revokes/deletes the stored token behind a confirm
