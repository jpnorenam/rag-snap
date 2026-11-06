# answer-judge Specification

## Purpose
Judging of batch answers with Gemini: a per-answer verdict against the evidence each answer was written from, a cross-answer consistency pass, and how verdicts are recorded and summarised for the human reviewer.
## Requirements
### Requirement: Judging is on by default and can be turned off

An `answer batch` run SHALL judge its answers by default, provided a judge model resolves and `GEMINI_API_KEY` is present in the environment. `judge.enabled` SHALL default to true. The judge model SHALL be `judge.model` when set, otherwise `analysis.model`. When `judge.enabled` is false, the run SHALL behave exactly as it did before judging existed, and its results SHALL carry no verdict fields. When judging is enabled but another prerequisite is missing, the run SHALL report every missing prerequisite in one notice before answering starts, including how to turn judging off. It SHALL then answer the batch without judging, and SHALL NOT fail. A run started with `--no-judge` on the CLI, or with `judge: false` over the API, SHALL NOT judge, whatever the configuration.

#### Scenario: Judged by default

- **WHEN** a batch runs on an install with a Gemini model configured and `GEMINI_API_KEY` set, and no judge key changed
- **THEN** every answer is judged

#### Scenario: Judging disabled

- **WHEN** `judge.enabled` is false and a batch runs
- **THEN** no Gemini call is made
- **AND** the results file has the same shape as before judging existed

#### Scenario: Enabled but no key

- **WHEN** `judge.enabled` is true and `GEMINI_API_KEY` is unset
- **THEN** the run reports that the key is missing before the first question
- **AND** it answers every question and writes unjudged results

#### Scenario: Model falls back to the analysis model

- **WHEN** `judge.model` is empty and `analysis.model` is set
- **THEN** the judge calls the `analysis.model` model

#### Scenario: Per-run opt-out

- **WHEN** judging is fully configured and the batch is run with `--no-judge`
- **THEN** no answer is judged

### Requirement: Each answer is judged against the evidence it was generated from

The judge SHALL evaluate each answer against inputs drawn from the same run:
- the question, its id and its applied domain context;
- the batch's resolved system prompt;
- the answer itself;
- the exact retrieved context the answer was generated from.

The retrieved context SHALL be taken from the running batch, and SHALL NOT be retrieved again. The judge SHALL NOT be asked to state, as fact, any correction drawn from its own knowledge. Where the judge doubts a claim that the context supports, it SHALL flag the claim for verification and SHALL NOT assert a replacement value. The retrieved context SHALL NOT be written to the results file.

#### Scenario: Unsupported claim is caught

- **WHEN** an answer states a figure that appears nowhere in its retrieved context
- **THEN** the verdict scores it 3 or lower
- **AND** its comment quotes the figure and says it is not supported by the retrieved context

#### Scenario: Judge doubts a supported claim

- **WHEN** an answer repeats a claim that its context states, and that claim is internally inconsistent
- **THEN** the comment asks the reviewer to verify the claim against its source
- **AND** the comment does not supply a corrected value as fact

#### Scenario: Context is not persisted

- **WHEN** a judged batch writes its results
- **THEN** the results file contains no retrieved context

### Requirement: Verdicts follow an anchored rubric

Each verdict SHALL carry four fields:
- an integer `confidence` from 1 to 5;
- a `rating` derived from `confidence`;
- a `comment`;
- a list of `issues` drawn from a fixed set of categories.

The rubric SHALL anchor each score:
- 5 — every claim is supported, the question is fully answered and the prompt's rules are followed; paste it as is.
- 4 — correct and supported, with a light edit: wording, a preamble, a sentence too many, or a format slip.
- 3 — it needs a real edit or a check: a claim or figure to verify, part of the question unanswered, or a sentence that must come out because it volunteers a limitation or breaks a rule its section set.
- 2 — it is wrong in a way that matters: an unsupported or contradicted claim, a different question answered, or another customer, bid or proposal named.
- 1 — it is wrong or fabricated in substance, or it is a gap.

A problem fixed by deleting or rewording a phrase SHALL NOT score below 3, except naming another customer, bid or proposal. A specific figure, duration, version, release name or command example that looks malformed or implausible to the judge SHALL be flagged for verification at 3, even when the context states it.

The rating SHALL be green for 5, yellow for 4 or 3, and red for 2 or 1.

The comment SHALL be empty when `confidence` is 5. Otherwise it SHALL name the specific sentence, claim or omission and the action the reviewer must take. The issue categories SHALL be `unsupported_claim`, `contradicts_context`, `missed_context`, `incomplete`, `off_question`, `other_customer`, `rule_violation` and `gap`. A model response that is not a valid verdict SHALL be treated as a failed judgment, and SHALL NOT be coerced into a verdict.

#### Scenario: Clean answer

- **WHEN** the judge finds no problem with an answer
- **THEN** its verdict has confidence 5, rating green, an empty comment and no issues

#### Scenario: A deletable sentence is not red

- **WHEN** an answer's only problem is one sentence that volunteers a limitation
- **THEN** the verdict is yellow, with confidence 3

#### Scenario: Another customer is named

- **WHEN** an answer names another operator's RFI or proposal that appears in its context
- **THEN** the verdict is rated red with the `other_customer` issue
- **AND** the comment names the reference to remove

#### Scenario: Comment present on a non-perfect score

- **WHEN** a verdict's confidence is below 5
- **THEN** its comment is not empty

#### Scenario: Invalid verdict

- **WHEN** the model returns a confidence outside 1 to 5
- **THEN** that row is recorded as not judged, with the reason

### Requirement: Gaps and mechanical checks are decided locally

A result whose answer is the fixed no-context response SHALL always receive a red verdict, because the reviewer has nothing to paste. When retrieval returned nothing, the verdict SHALL carry the `gap` issue and a comment saying that no grounding context was retrieved, and it SHALL be produced without a model call. When context was retrieved and the answering model wrote the fixed response itself, the answer SHALL still be judged, so the judge can report `missed_context`. Whatever the judge returns, the verdict SHALL then be red, with confidence 1, and SHALL carry `gap` unless the judge reported `missed_context`.

Before judging an answer, the run SHALL detect markdown syntax in it locally. Any markdown found SHALL be recorded as a `rule_violation` in the verdict comment, and the verdict's confidence SHALL be capped at 4.

The answer's word and sentence counts SHALL be computed locally and passed to the judge as stated facts, so the judge compares them against the prompt's length rule instead of counting. A leading declaration of at most three words, such as "Supported.", SHALL be reported as an opener and SHALL NOT be counted as a sentence. Markdown SHALL be reported only by the local check, never by the judge.

#### Scenario: No-context answer

- **WHEN** an answer is the fixed no-context response
- **THEN** its verdict is red with the `gap` issue
- **AND** no Gemini call is made for it

#### Scenario: Model-written refusal judged correct

- **WHEN** context was retrieved, the answering model wrote the fixed no-context response, and the judge scores it 5
- **THEN** the verdict is red with confidence 1 and the `gap` issue

#### Scenario: Required opener is not a sentence

- **WHEN** an answer is "Supported." followed by five sentences, under a five-sentence limit
- **THEN** the judge is told it has an opener and five sentences
- **AND** its verdict does not report a length overrun

#### Scenario: Markdown leak

- **WHEN** an answer contains `**bold**`
- **THEN** its verdict comment names the markdown
- **AND** its confidence is at most 4

#### Scenario: A judged gap that the context could have answered

- **WHEN** the model wrote a refusal in its own words, but the retrieved context contains the answer
- **THEN** the verdict is red with the `missed_context` issue
- **AND** the comment points to the source that holds the answer

### Requirement: Answers are judged in parallel while the batch is still answering

Each answer SHALL be queued for judging as soon as it is produced, while later questions are still being answered. The number of concurrent judge calls SHALL NOT exceed `judge.concurrency`, which defaults to 4.

A call rejected for quota or rate limiting, or failing with a transient server error, SHALL be retried with backoff a bounded number of times before the row is marked not judged. Cancelling the run SHALL stop judging cooperatively.

The order of results SHALL NOT depend on the order in which judgments complete.

#### Scenario: Judging overlaps answering

- **WHEN** a batch of many questions runs with judging enabled
- **THEN** answers already produced are judged while later questions are still being answered

#### Scenario: Concurrency bound

- **WHEN** `judge.concurrency` is 4
- **THEN** no more than four judge calls are in flight at once

#### Scenario: Rate limited

- **WHEN** a judge call is rejected with HTTP 429
- **THEN** it is retried after a delay
- **AND** the row is marked not judged only after the retries are exhausted

### Requirement: A consistency pass compares answers across the run

After every answer has been judged, the run SHALL make one consistency call. That call SHALL receive every answered result's position, id, question and answer, plus the system prompt, and no retrieved context.

The call SHALL report contradictions only, and SHALL NOT re-report rule violations such as markdown, length or customer naming, which are already reported on each answer. Each finding SHALL name the rows involved and, for each row, quote the phrase in that row's answer that takes part in the contradiction, together with a comment saying what conflicts and what the reviewer should reconcile.

Each finding SHALL be verified locally before it is used:
- a row outside the run SHALL be dropped;
- a row whose quoted phrase does not occur in that row's answer, compared ignoring case and runs of whitespace, SHALL be dropped;
- a finding left with fewer than two rows, or with an empty comment, SHALL be discarded.

The number of findings discarded SHALL be recorded.

A kept finding with more than 8 rows is broad. A broad finding SHALL NOT be attached to any row and SHALL NOT change any row's confidence. It SHALL be kept only at run level.

Every other kept finding SHALL add to each row it names a short note. The note SHALL name the other rows in the finding and carry the finding's comment. It SHALL NOT repeat the row's own number. The confidence of each of those rows SHALL then be capped at 3, which makes the row at most yellow. When the cap lowers a row's confidence, the verdict SHALL record the confidence it had before the pass.

If the consistency call fails, the per-answer verdicts SHALL be kept unchanged, and the failure SHALL be reported.

#### Scenario: Contradicting figures

- **WHEN** one answer states 36,000 packages and another states 25,000 for the same repository, and the finding quotes each figure from its row
- **THEN** both rows carry a note that names the other row and the conflict
- **AND** neither row is rated green
- **AND** a row that was green before the pass records its earlier confidence

#### Scenario: Quoted phrase is not in the answer

- **WHEN** a finding names rows 37 and 52 but the phrase quoted for row 52 does not occur in row 52's answer
- **THEN** row 52 is dropped, the finding is left with one row and is discarded, and the discarded count is incremented
- **AND** neither row's verdict changes

#### Scenario: Run-wide remark returned as a finding

- **WHEN** the consistency call returns one finding about customer names that names 284 rows, each with a phrase found in its answer
- **THEN** the finding is kept at run level and marked broad
- **AND** no row's comment or confidence changes

#### Scenario: Consistency call fails

- **WHEN** the consistency call errors
- **THEN** every per-answer verdict is retained
- **AND** the run reports that the consistency pass did not complete

### Requirement: Judging never loses the answers

A failure of the judge SHALL NOT fail the batch run or discard any answer. The CLI SHALL write its results file once answering completes and before judging finishes. It SHALL then rewrite the same file with the verdicts. A row whose judgment failed SHALL carry a verdict with no confidence, no rating, and an error reason. It SHALL be counted as not judged.

#### Scenario: Judge unreachable mid-run

- **WHEN** every judge call fails because the Gemini API is unreachable
- **THEN** the results file still holds every answer
- **AND** each row is marked not judged with the reason

#### Scenario: Interrupted during judging

- **WHEN** the process is interrupted after answering completes but before judging completes
- **THEN** the results file on disk holds every answer

### Requirement: Verdicts are recorded on the results and summarised for the reviewer

Each judged `BatchResult` SHALL carry its verdict under a `judgment` key. The `BatchOutput` SHALL carry a `judge` summary holding:
- the judge model;
- the count of green, yellow, red and not-judged rows;
- whether the consistency pass completed;
- every kept consistency finding, with its rows, each row's quoted phrase, its comment, and whether it is broad;
- the number of consistency findings discarded by verification.

Both keys SHALL be omitted when judging did not run. A verdict whose confidence the consistency pass lowered SHALL carry the confidence from before the pass.

When a judged run completes, the CLI SHALL print the counts. It SHALL then list every red row and every yellow row, identified by spreadsheet row number, with the id in brackets and the comment. The first result is row 2. The summary SHALL state that row 2 is the first answer. When there are kept consistency findings, the summary SHALL list each one once, in its own section, with its rows and comment, marking broad findings as not applied to rows. It SHALL state how many findings were discarded when any were.

#### Scenario: Results carry verdicts

- **WHEN** a judged batch completes
- **THEN** every result in the file has a `judgment`
- **AND** the file has a `judge` summary whose counts sum to the number of results

#### Scenario: Terminal summary by row

- **WHEN** the third result is rated red
- **THEN** the summary lists it as row 4 with its id and comment

#### Scenario: Findings printed once

- **WHEN** a judged run has one applied finding over rows 47 and 263 and one broad finding
- **THEN** the terminal summary has a consistency section listing each finding once
- **AND** the broad finding is marked as not applied to rows

#### Scenario: Unjudged results keep their shape

- **WHEN** a batch runs with judging disabled
- **THEN** its results file has no `judgment` or `judge` keys

### Requirement: Judge traffic goes only to the pinned Gemini endpoint

The judge SHALL call Google's Gemini API at the same compiled-in endpoint analysis uses, with the API key read only from `GEMINI_API_KEY`. Host variables that would redirect or re-authenticate the SDK SHALL be inert, exactly as for analysis. The judge SHALL NOT fall back to `CHAT_API_KEY` or to the chat backend. Verbose output and error messages SHALL NOT contain the key.

#### Scenario: Base URL override is ignored

- **WHEN** `GOOGLE_GEMINI_BASE_URL` is set in the environment and a judged batch runs
- **THEN** judge requests go to the compiled-in Gemini endpoint

#### Scenario: No chat-key fallback

- **WHEN** `GEMINI_API_KEY` is unset and `CHAT_API_KEY` is set
- **THEN** the batch is not judged
- **AND** the chat key is never sent to Google

