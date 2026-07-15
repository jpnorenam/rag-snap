## Why

The browser UI's Change-2 import flow only accepts a local `.tar.gz` upload; users who keep their exported knowledge-base archives in Google Drive are told to drop to the CLI (`k import --url <drive-url>`). The CLI already has full Drive parity (interactive picker + `--all`), but that flow is client-side and interactive — the daemon deliberately does not expose it. This change brings Drive import to the daemon and the UI so the browser reaches parity with `k import --url`, removing the "use the CLI for Drive" escape hatch.

## What Changes

- **Daemon Drive OAuth, mediated over REST.** The daemon gains a Drive OAuth flow it can drive on behalf of the browser: it produces a Google consent URL (loopback redirect, PKCE), runs the callback, exchanges the code, and stores the resulting token on the machine. New endpoints let the UI start the flow, poll for completion, read connection status (connected account + whether Drive is configured at all), and disconnect (delete the stored token).
- **Daemon Drive resolution + import.** New endpoints resolve a Drive folder/file URL (the same URL forms the CLI accepts) into a list of discovered `.tar.gz` archives (name · size), and start an import as a tracked async operation per selected archive — reusing the existing `ImportKnowledgeBase` core. Unlike local-file import (client-side, needs the home directory the confined daemon can't reach), Drive import is network + OpenSearch only, so the daemon can perform it.
- **UI import modal gains a Drive source.** The `/knowledge/` import modal becomes a two-source chooser (**From file** | **From Google Drive**), matching the ingest modal's tab pattern. The Drive path is a multi-step modal: connect → locate → pick archives (with select-all = `--all`, and a force checkbox) → import. The "Importing from Google Drive? Use the CLI…" hint added in Change 2 is removed.
- **Config-gated feature.** When `gdrive.client.id`/`gdrive.client.secret` are unset the Drive tab renders an information state pointing at those package config keys and the Status page, rather than offering a dead connect button.
- **Docs.** `docs/usage.md` (Drive URL forms already documented there) and the UX doc `docs/ux/07-gdrive-import.md` are the reference; `rest-api.yaml` gains the new endpoints.

No new config keys — this reuses the existing `gdrive.client.id` / `gdrive.client.secret` (`package`-scoped) and the OAuth token storage the CLI already uses. No new secrets/env vars.

## Capabilities

### New Capabilities
- `rest-api-gdrive-import`: daemon REST endpoints for the Google Drive OAuth lifecycle (start/status/disconnect), Drive URL resolution into archive listings, and starting Drive-archive imports as tracked operations. Touches **OpenSearch** (the import target) and Google's OAuth/Drive HTTP APIs; does not touch the inference server or Tika.

### Modified Capabilities
- `local-ui-app`: the knowledge-management import modal gains a Google Drive source (multi-step connect/locate/pick/import), a Drive `NavIcon`, a Drive-not-configured information state, and disconnect; the Change-2 "use the CLI for Drive" hint is removed.

## Impact

- **New daemon code**: `internal/api/handlers_gdrive.go` (+ routes in `internal/api/server.go`), reusing `cmd/cli/basic/knowledge` Drive helpers (`ParseDriveURL`, `ListDriveArchives`, `GetDriveFileName`, `DownloadDriveArchive`, `ImportKnowledgeBase`) and a refactor of the CLI loopback OAuth flow so the daemon can run it non-interactively.
- **New UI code**: a `GdriveImportModal` (or an extended `ImportModal`) under `ui/components/knowledge/`, a `ui/lib/api/gdrive.ts` client module, a Google `NavIcon`, and Drive-specific styles in `ui/app/globals.scss`.
- **APIs**: `rest-api.yaml` and the OpenSearch client are the touched surfaces; the inference server and Tika are untouched.
- **Runtime/confinement**: relies on the daemon being able to bind an ephemeral loopback port for the OAuth callback and write the token under its own data dir — to be validated against strict confinement (see design.md).
- **Docs**: `rest-api.yaml`, `docs/usage.md`, and `openspec/specs/{rest-api-gdrive-import,local-ui-app}`.
