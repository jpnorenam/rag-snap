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

#### Scenario: Mid-session prompt edits do not affect the running session

- **WHEN** a stored prompt is updated while a chat session is in progress
- **THEN** the running session continues with the prompts it started with
- **AND** the next session started uses the updated prompts
