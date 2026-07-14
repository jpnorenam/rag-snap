# rest-api-chat Specification (delta)

## MODIFIED Requirements

### Requirement: RAG grounding matches the existing chat loop

Answer generation over the API SHALL use the same retrieval-augmented pipeline as the existing
chat REPL — query rewriting into retrieval keywords, hybrid retrieval over the active bases,
prompt augmentation with the retrieved context, and the same grounding/provenance rules — so
that answers are equivalent to the CLI experience.

The prompt templates driving generation SHALL come from the daemon prompt store
(`rest-api-prompts`): the session's system prompt is the stored `chat_system_prompt`, resolving
to its built-in default when not customized. (The `source_rules` template governs batch answering
with a custom manifest prompt, not chat — chat's grounding rules live inside
`chat_system_prompt`, matching the chat REPL.) Prompts SHALL be resolved when the session starts;
changes to stored prompts SHALL apply to sessions started afterwards and SHALL NOT alter a
session already in progress.

A **customized** `chat_system_prompt` SHALL be honoured whether or not retrieval is available —
user configuration is never silently overridden. When retrieval is unavailable and the prompt is
**not** customized, the session SHALL fall back to a generic assistant prompt instead of the
built-in default, because the default is written for RAG (it instructs the model to answer only
from retrieved context, which would make it refuse every question).

#### Scenario: Grounded answer over the API

- **WHEN** a client asks a question with knowledge bases active
- **THEN** the daemon rewrites the query, retrieves context via the hybrid pipeline, augments the prompt, and streams a grounded answer
- **AND** the grounding and provenance behavior matches the existing chat REPL

#### Scenario: Chatting without active knowledge bases

- **WHEN** a client sends a prompt with no knowledge bases active
- **THEN** the daemon responds without retrieval augmentation

#### Scenario: Customized prompts drive new sessions

- **WHEN** the stored `chat_system_prompt` is customized and a client starts a chat session
- **THEN** the session's system prompt is the customized template instead of the built-in default

#### Scenario: Customized prompt honoured without retrieval

- **WHEN** the stored `chat_system_prompt` is customized and a client starts a chat session while
  retrieval is unavailable
- **THEN** the session's system prompt is the customized template

#### Scenario: Uncustomized default falls back without retrieval

- **WHEN** the `chat_system_prompt` is not customized and a client starts a chat session while
  retrieval is unavailable
- **THEN** the session's system prompt is a generic assistant prompt rather than the RAG-specific
  built-in default

#### Scenario: Mid-session prompt edits do not affect the running session

- **WHEN** a stored prompt is updated while a chat session is in progress
- **THEN** the running session continues with the prompts it started with
- **AND** the next session started uses the updated prompts
