# Add user-defined knowledge labels

## Why

Provenance labels (`[CANONICAL]`, `[KAPA-CANONICAL]`, `[UPSTREAM]`) are currently hardcoded: they are inferred at retrieval time from an index-naming convention (a base name containing "upstream") in three separately-maintained places, and the default system prompts bake in both the tag names and Canonical-specific priority rules. Users cannot label their own content, and the labels a prompt references are not data anywhere in the system. With prompt variants now shipped, users can customize augmentation behavior — but they cannot define the labels those prompts should act on. This change makes labels user-managed metadata attached to knowledge, so custom system prompts can give prelation to content based on labels the user chose.

## What Changes

- **Per-source labels with a per-base default.** A knowledge base carries a default label; `k ingest --label <label>` overrides it per source. Exactly one label per source (single-label model). The label is stored in the source's metadata and denormalized onto every chunk document at ingest time (new `label` keyword field in the index template), so search hits carry it directly and `k export`/`import` moves labels with the data.
- **Label semantics live in prompts, not in a registry.** The system only guarantees each retrieved chunk is tagged `[<LABEL>]` in the RAG context. What a label *means* and how labels rank against each other is expressed in the user's system prompts, managed through the existing prompt-variant machinery. The shipped default prompts keep referencing the default label set (`canonical`, `kapa-canonical`, `upstream`) so stock behavior is unchanged.
- **Labeling commands.** `k create <name> --label <label>` sets the base default (falls back to `canonical`, or `upstream` when the name contains "upstream", preserving today's convention); new `k label <base> [<label>]` shows or changes the default, with `--apply-to-existing` backfilling already-ingested chunks via `_update_by_query`.
- **Read-time fallback for unlabeled data.** Chunks without a `label` field resolve their label from the index name exactly as today (`upstream` substring → `upstream`, else `canonical`), so existing bases keep working without migration. Kapa.ai hits (never indexed) carry the fixed implicit label `kapa-canonical`.
- **One label resolver.** The duplicated logic in `chat.sourceLabel`, `api.provenanceLabel`, and `remoteProvenanceLabel` collapses into a single resolver in the `knowledge` package used by the REPL, the daemon, and remote clients.
- **BREAKING (REST API):** search hits replace the derived `provenance` field (`canonical`/`upstream`) with a `label` field carrying the stored/resolved label. The bundled UI is updated in the same change; external API consumers must adapt.
- **User-facing surfaces changed:** `k create` gains `--label`; new `k label` subcommand; `k ingest` (and batch ingest manifest entries) gain `--label`/`label:`; `k sources` and source inspection show the label; `/search` in chat, `k search`, and the UI Search page display the stored label instead of the inferred tag; UI create-KB and ingest forms gain a label input. README command documentation must be updated accordingly.

Out of scope: multi-label sources, a label registry with descriptions/priorities, retrieval-time weighting by label, and any change to the prompt-variant system itself.

## Capabilities

### New Capabilities

- `knowledge-labels`: the labeling model — how labels are declared (base default, per-source override), stored (source metadata + denormalized chunk field), resolved (stored value, index-name fallback, kapa implicit), surfaced in RAG context as `[<LABEL>]` tags, and managed (`k label`, backfill).

### Modified Capabilities

- `chat-search`: results display the resolved user label instead of the fixed `[CANONICAL]`/`[UPSTREAM]` provenance tag.
- `rest-api-knowledge`: base create accepts a default label and base/source responses expose labels; ingest accepts a per-source label; search hits carry `label` instead of derived `provenance`.
- `local-ui-app`: Search results footer shows the stored label; knowledge create/ingest forms accept a label; base detail and sources list show labels.

## Impact

- **Services touched:** OpenSearch only — index template mapping gains a `label` keyword field; chunk documents and the `rag-snap-metadata` sources index gain label fields; backfill uses `_update_by_query`. The inference server is unaffected structurally (prompt text still references labels); Tika is untouched.
- **Config:** no new config keys (base default labels live in the knowledge store so they travel with export/import; snapctl config is not involved). No new secrets.
- **Code:** `cmd/cli/basic/knowledge` (bulk `Document`, `SourceMetadata`, ingest, search, new label ops, batch manifest), `cmd/cli/basic/chat` (`rag.go` context formatting, `search.go`, `remote.go`), `internal/api` (`handlers_search.go`, knowledge/ingest handlers, API types), `internal/apiclient`, `ui/` (search results, knowledge forms), default prompt text constants (wording only — same default labels).
- **Compatibility:** existing bases and exports work unchanged via the read-time fallback; already-saved custom prompt variants keep functioning because default label names are preserved.
