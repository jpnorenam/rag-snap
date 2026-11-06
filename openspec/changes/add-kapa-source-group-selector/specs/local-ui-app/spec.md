## ADDED Requirements

### Requirement: Kapa.ai source groups can be selected in chat, distinct from local bases

The chat screen SHALL let the user choose which kapa.ai source groups are active for the
session, using a selector row structurally and visually distinct from the local knowledge-base
selector (its own label and its own group of toggle chips), so the user can always tell which
retrieval source a given toggle affects. It SHALL list the project's groups by display name from
the source-group listing endpoint defined by `rest-api-kapa`, and SHALL apply changes mid-session
by sending the source-group control message over the chat websocket, without restarting the
session. A new session SHALL start with no groups selected; a resumed session SHALL show the
selection restored from the saved chat.

The selector SHALL follow the same view states as the answer-batch screen's source-group picker:
loading, loaded, empty (configured but the project has no groups), error (the listing failed,
with a retry), and unconfigured — which SHALL render as a hint that credentials are required, not
as an error and not as an empty picker. When the daemon reports that a selection could not be
applied, the screen SHALL say so rather than showing the groups as active.

#### Scenario: List available kapa.ai source groups

- **WHEN** the chat screen loads and kapa.ai is configured
- **THEN** the project's source groups are listed by display name in their own selector row, separate from the knowledge-base selector row
- **AND** no group is selected for a new session

#### Scenario: Switch active kapa.ai source groups mid-session

- **WHEN** the user changes the kapa.ai source-group selection during a session
- **THEN** the UI sends the source-group control message over the open websocket
- **AND** the session continues without reconnecting

#### Scenario: Selecting kapa.ai groups does not change local bases

- **WHEN** the user toggles a kapa.ai source-group chip
- **THEN** the active knowledge-base selection is unaffected

#### Scenario: Integration not configured in chat

- **WHEN** the chat screen loads and the listing reports kapa.ai as unconfigured
- **THEN** the kapa.ai row shows a hint that credentials are required instead of an empty picker

#### Scenario: Listing fails in chat

- **WHEN** the source-group listing request fails
- **THEN** the kapa.ai row surfaces an error with a retry rather than an empty picker
- **AND** the knowledge-base selector and chat remain usable

#### Scenario: Resumed selection is shown

- **WHEN** the user resumes a saved chat that had kapa.ai source groups selected
- **THEN** those groups show as selected, or the screen reports that the saved selection could not be applied

### Requirement: Kapa.ai source groups can be selected in Search, distinct from local bases

The Search page SHALL let the user choose which kapa.ai source groups are included in a search,
using the same selector row and the same view states as the chat screen. Selecting one or more
groups SHALL include exactly those group ids in the `POST /1.0/search` request; selecting none
SHALL omit kapa.ai from the request, matching how the knowledge-base chips scope the local part
of the search. The selection SHALL round-trip through the page's URL parameters alongside the
query, bases, and result count, so a shared or reloaded search URL reproduces the same scope.
When the search response reports that kapa.ai could not be applied, the page SHALL show that
notice alongside the local results.

#### Scenario: List available kapa.ai source groups on the Search page

- **WHEN** the Search page loads and kapa.ai is configured
- **THEN** the project's source groups are listed by display name in their own selector row, separate from the knowledge-base selector row

#### Scenario: Searching with kapa.ai groups selected

- **WHEN** the user selects one or more kapa.ai source-group chips and submits a search
- **THEN** the UI issues `POST /1.0/search` including exactly those group ids

#### Scenario: Searching with no kapa.ai groups selected

- **WHEN** the user submits a search with no kapa.ai source-group chips selected
- **THEN** the UI issues `POST /1.0/search` without kapa.ai group ids, searching local bases only

#### Scenario: Integration not configured on the Search page

- **WHEN** the Search page loads and the listing reports kapa.ai as unconfigured
- **THEN** the kapa.ai row shows a hint that credentials are required instead of an empty picker

#### Scenario: Kapa.ai could not be applied to a search

- **WHEN** a search response reports that the requested kapa.ai scope could not be applied
- **THEN** the page shows the local results together with a notice that kapa.ai results are missing

## MODIFIED Requirements

### Requirement: Search results render full chunks with score and provenance

Each hit SHALL render as one card in ranked order showing: a header with the rank number,
the source ID, the originating source as a non-interactive chip — the knowledge base name for a
local hit, or a fixed "kapa.ai" label for a kapa.ai hit — and the relevance score right-aligned
to 3 decimals; the chunk's full content preserving paragraph breaks and without truncation; and
a footer with provenance details in small text. The results region SHALL be announced via
`aria-live="polite"` as "N results", be preceded by an off-screen "Results" heading, and focus
SHALL remain in the query input after submit. The source ID SHALL render as plain text until a
knowledge-detail route exists to link to; a kapa.ai hit's source ID (a URL) SHALL never link to
the knowledge-detail route.

#### Scenario: Result card contents

- **WHEN** a search returns hits
- **THEN** each card shows rank, source ID, KB name chip, and the score to 3 decimals
- **AND** the complete chunk content renders untruncated with paragraph breaks preserved
- **AND** the output matches what `k search` prints for the same query (chunks, scores, provenance)

#### Scenario: Results announced to assistive tech

- **WHEN** a search completes with N hits
- **THEN** a polite live region announces "N results" and focus is still in the query input

#### Scenario: Kapa.ai result cards are distinguishable from local ones

- **WHEN** a search's results include one or more kapa.ai hits
- **THEN** each kapa.ai hit's card shows a fixed "kapa.ai" chip in place of a knowledge-base name chip
- **AND** its footer provenance details show the `kapa-canonical` label
