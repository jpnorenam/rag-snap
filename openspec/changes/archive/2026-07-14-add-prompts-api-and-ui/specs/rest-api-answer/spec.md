# rest-api-answer Specification (delta)

## MODIFIED Requirements

### Requirement: Batch answering runs as an operation

The API SHALL provide `POST /1.0/answer/batch` that accepts a batch manifest of questions and
the knowledge bases to use, and runs them as an asynchronous operation. Each question SHALL be
answered using the same RAG+LLM pipeline as the chat answer path (keyword rewrite merged with any
manifest keywords, hybrid retrieval, grounded generation). When no context is retrieved for a
question, the answer SHALL be the fixed "not enough information" response rather than an
ungrounded generation.

The prompt templates driving generation SHALL come from the daemon prompt store
(`rest-api-prompts`): the stored `answer_system_prompt` and `source_rules`, each resolving to
its built-in default when not customized. Prompts SHALL be resolved when the batch operation
starts; changes to stored prompts SHALL apply to operations started afterwards and SHALL NOT
alter an operation already running.

The operation's metadata SHALL convey progress across the questions, and the operation SHALL be
cancellable.

#### Scenario: Running a batch manifest

- **WHEN** a client posts a manifest of questions to `POST /1.0/answer/batch`
- **THEN** the API returns an asynchronous operation
- **AND** the operation answers each question via the RAG+LLM pipeline and reports progress

#### Scenario: A question with no retrieved context

- **WHEN** a question in the batch retrieves no grounding context
- **THEN** its answer is the fixed "not enough information" response, not an ungrounded generation

#### Scenario: Cancelling a batch run

- **WHEN** a client cancels a running batch operation
- **THEN** processing stops cooperatively and the operation reports cancellation

#### Scenario: Customized prompts drive new batch runs

- **WHEN** the stored `answer_system_prompt` or `source_rules` is customized and a client starts
  a batch operation
- **THEN** the operation's generation uses the customized templates instead of the built-in
  defaults

#### Scenario: Mid-run prompt edits do not affect the running operation

- **WHEN** a stored prompt is updated while a batch operation is running
- **THEN** the running operation continues with the prompts it started with
- **AND** the next batch operation started uses the updated prompts
