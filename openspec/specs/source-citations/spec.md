# source-citations Specification

## Purpose

Trace every RAG answer back to the documents it was grounded on, by resolving retrieved hits to
document-level citations and requiring grounded answers to cite them.

## Requirements

### Requirement: Every retrieved hit carries a document-level citation
Every hit returned by knowledge retrieval SHALL carry a citation with: a `key` identifying the
cited document, a non-empty human-readable `label`, an optional `href` link, a `locator` (empty in
this release, reserved for section/cell references), and the hit's knowledge base and label tag.
Citation resolution SHALL NOT fail a retrieval.

#### Scenario: Local hit gets a citation
- **WHEN** a search over a local knowledge base returns a chunk with source id `rfp-2024`
- **THEN** the hit carries a citation whose `key` is `rfp-2024`, whose `label` is non-empty, and whose knowledge base is the base the chunk came from

#### Scenario: Metadata lookup unavailable
- **WHEN** the source-metadata store cannot be read during retrieval
- **THEN** retrieval still returns its hits, each with a citation whose `label` is the hit's source id and whose `href` is empty

### Requirement: Citation identity is the document
A citation's `key` SHALL identify the source document, not the chunk: chunks of the same source
SHALL share one `key`. In this release the `key` SHALL be the hit's source id.

#### Scenario: Two chunks of one document
- **WHEN** retrieval returns three chunks, two of which belong to source `guide.pdf`
- **THEN** the two `guide.pdf` chunks carry citations with the same `key`, and the hits resolve to two distinct citations

### Requirement: Citation link resolves only from recorded URLs
A citation's `href` SHALL be taken, in order of preference, from: the Kapa source URL for Kapa
hits; the source's recorded `source_uri`; the source's recorded file path when it is an `http://`
or `https://` URL. Otherwise `href` SHALL be empty. A link SHALL NOT be derived from a source id.

#### Scenario: URL-ingested source
- **WHEN** a hit's source was ingested from `https://ubuntu.com/pro` and has no `source_uri`
- **THEN** the citation's `href` is `https://ubuntu.com/pro`

#### Scenario: Recorded source URI wins
- **WHEN** a source has `source_uri` `https://github.com/canonical/k8s-snap/blob/main/README.md` and a local file path
- **THEN** the citation's `href` is the `source_uri`

#### Scenario: Repo source ingested before this change
- **WHEN** a hit's source id is `canonical/k8s-snap/docs/install.md` and its metadata has no `source_uri` and a non-URL file path
- **THEN** the citation's `href` is empty and no GitHub link is constructed

#### Scenario: Kapa hit
- **WHEN** a Kapa result has source URL `https://documentation.ubuntu.com/server/`
- **THEN** its citation's `href` is that URL

### Requirement: Citation label falls back through recorded metadata
A citation's `label` SHALL be, in order of preference: the source's extracted title when it passes
a sanity check; the source's recorded file name when it is not a temporary file name; for Kapa
hits, the source URL; otherwise the source id.

#### Scenario: Usable title
- **WHEN** a source's metadata has title `Ubuntu Pro Service Description`
- **THEN** the citation's `label` is `Ubuntu Pro Service Description`

#### Scenario: Junk title is skipped
- **WHEN** a source's title is empty, equals its file name, or is a word-processor placeholder such as `Microsoft Word - draft.docx`, and its file name is `service-description.pdf`
- **THEN** the citation's `label` is `service-description.pdf`

#### Scenario: Temporary file name is skipped
- **WHEN** a source's recorded file name is a temporary download name and it has no usable title
- **THEN** the citation's `label` is the source id

#### Scenario: Imported base without metadata
- **WHEN** a hit comes from a base imported from an archive that carried no source metadata
- **THEN** the citation's `label` is the source id and its `href` is empty

### Requirement: Context chunks carry per-document citation markers
Each retrieved chunk supplied to the model SHALL be prefixed with a marker `[<n>]` identifying its
cited document. Chunks sharing a citation `key` SHALL share a marker. The model SHALL be instructed,
alongside the retrieved context and only when context was retrieved, to end each paragraph that uses
it with the markers it relied on.

#### Scenario: Shared marker for one document
- **WHEN** the context holds two chunks of `guide.pdf` and one chunk of `faq.md`
- **THEN** both `guide.pdf` chunks are prefixed with the same marker and the `faq.md` chunk with a different one

#### Scenario: Custom system prompt keeps the citation instruction
- **WHEN** the session or batch uses a customized system prompt and a turn retrieves context
- **THEN** the citation instruction is still supplied to the model with that context

#### Scenario: No citation instruction without retrieved context
- **WHEN** a turn is answered with no retrieved context (no active knowledge source, or retrieval found nothing)
- **THEN** the model receives no citation instruction for that turn, and the session's system prompt is the resolved prompt unchanged

### Requirement: Chat markers are numbered session-wide
Within one chat session a document SHALL keep the same marker number for the life of the session,
including across turns and after the session is resumed from a saved chat. A document first seen in
a later turn SHALL receive the next unused number.

#### Scenario: Document cited in two turns
- **WHEN** `guide.pdf` is marked `[1]` in turn 1 and is retrieved again in turn 3 alongside a new document
- **THEN** `guide.pdf` is still `[1]` in turn 3 and the new document gets the next unused number

#### Scenario: Resumed session keeps numbering
- **WHEN** a saved chat whose turns cited `[1]` and `[2]` is resumed and a new document is retrieved
- **THEN** the new document is numbered `[3]`

### Requirement: Answers cite only retrieved documents
After generation, markers in the answer that do not match a document retrieved for that turn SHALL
be removed from the answer. The answer's sources SHALL be exactly the documents whose markers remain,
in first-cited order.

#### Scenario: Invented marker dropped
- **WHEN** the turn's context holds `[1]` and `[2]` and the model's answer cites `[1]` and `[7]`
- **THEN** `[7]` is removed from the answer and the answer's sources list only the `[1]` document

#### Scenario: Marker in a turn without retrieval
- **WHEN** a turn retrieved no documents and the model's answer contains `[1]`
- **THEN** the marker is removed from the answer and the answer carries no sources

#### Scenario: Retrieved but uncited document
- **WHEN** three documents were retrieved and the answer cites only `[2]`
- **THEN** the answer's sources list only the `[2]` document

#### Scenario: Bracketed numbers that are not markers
- **WHEN** the answer contains `arr[1]`, or `[1, 2]` inside inline or fenced code
- **THEN** those are left unchanged and are not counted as citations

#### Scenario: S-prefixed marker
- **WHEN** the model writes `[S1]` or `[S1, S2]` and documents 1 and 2 were retrieved
- **THEN** they are normalized to `[1]` and `[1][2]` and counted as citations

### Requirement: Chat answers without citations are flagged as unsourced
When retrieval supplied context for a chat turn and the answer, after invalid markers are removed,
cites no document, the answer SHALL be kept and recorded unchanged, SHALL carry no sources, and SHALL
be flagged as unsourced so every chat surface tells the user it has no relevant sources. Turns
answered without retrieval (no active knowledge source) SHALL NOT be flagged.

#### Scenario: Model cites nothing
- **WHEN** context was retrieved and the model's chat answer contains no valid marker
- **THEN** the recorded answer is the model's answer with empty sources, flagged unsourced

#### Scenario: No active knowledge base
- **WHEN** the user chats with no active knowledge source and the model replies without markers
- **THEN** the reply is kept unchanged, is not flagged, and carries no sources

### Requirement: Chat REPL shows the sources of each answer
After each grounded answer, the chat REPL (direct and remote modes) SHALL print a Sources block that
lists each cited document as its marker, label, and link when present, in first-cited order. When an
answer is flagged unsourced, the REPL SHALL print a "No relevant sources" notice instead of a
Sources block.

#### Scenario: Answer with sources
- **WHEN** an answer cites `[1]` (label `Ubuntu Pro Service Description`, link `https://ubuntu.com/pro`) and `[3]` (label `faq.md`, no link)
- **THEN** after the answer the REPL prints a Sources block with `[1] Ubuntu Pro Service Description — https://ubuntu.com/pro` and `[3] faq.md`

#### Scenario: Unsourced answer
- **WHEN** a grounded answer streamed to the terminal cited no retrieved document
- **THEN** the REPL leaves the streamed answer in place, prints `No relevant sources: this answer cites none of the retrieved documents.`, and the history records the answer

### Requirement: Chat sessions start with the default knowledge base active
A chat session started from the CLI — directly or through the daemon — SHALL start with the `default`
knowledge base active, so a first question is grounded without first selecting a base. A session
resumed from a saved chat SHALL use the saved set instead.

#### Scenario: CLI chat through the daemon
- **WHEN** the user runs `rag-cli.rag chat` while ragd is running
- **THEN** the session's active knowledge bases are `default` and the REPL says so

#### Scenario: Resumed chat
- **WHEN** a saved chat whose active bases were `docs` is resumed
- **THEN** the session's active bases are `docs`, not `default`

### Requirement: Ingest records a canonical source URI
Each ingested source SHALL record a `source_uri` when one is known: the URL for URL ingests; a full
web URL to the file at the fetched ref for GitHub and Gitea ingests; and the user-supplied URI for
file ingests. The source's recorded file path SHALL be the original input, never a temporary path.

#### Scenario: URL ingest
- **WHEN** a source is ingested from `https://ubuntu.com/pro`
- **THEN** its metadata records `source_uri` `https://ubuntu.com/pro`

#### Scenario: GitHub ingest
- **WHEN** file `docs/install.md` of `canonical/k8s-snap` is ingested from ref `main`
- **THEN** its metadata records `source_uri` `https://github.com/canonical/k8s-snap/blob/main/docs/install.md`

#### Scenario: File ingest with a supplied URI
- **WHEN** a user ingests a local file and supplies URI `https://example.com/spec.pdf`
- **THEN** its metadata records that `source_uri`

#### Scenario: File ingest without a URI
- **WHEN** a user ingests a local file without supplying a URI
- **THEN** its metadata records no `source_uri` and the citation's `href` is empty

#### Scenario: CLI batch ingest of a repo file
- **WHEN** a repo file is ingested through a CLI batch manifest
- **THEN** its recorded file path is the repo-relative path, not the temporary download path

### Requirement: Supplied source URIs are validated
A user-supplied source URI SHALL be accepted only if it is an absolute `http://` or `https://` URL.
Any other value SHALL be rejected before ingestion starts, with an error naming the expected form.

#### Scenario: Invalid URI rejected
- **WHEN** a user ingests a file supplying URI `file:///home/me/spec.pdf`
- **THEN** the ingest is rejected before extraction with an error stating an http(s) URL is required
