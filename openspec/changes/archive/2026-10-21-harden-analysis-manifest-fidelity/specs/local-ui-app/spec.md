## ADDED Requirements

### Requirement: A question-count shortfall is presented as a decision

When generation fails with a question-count shortfall, the build wizard SHALL present it as a decision
for the user rather than a dead end. It SHALL show both the estimated and the produced counts, and say
that the extraction finished normally, so the gap may be a disagreement about how finely to split rather
than missing questions. It SHALL show each section's produced count beside its estimate. It SHALL offer
an explicit action to continue with the produced questions, an action to re-run the extraction with
more reasoning, and an action to start again from upload.

Continuing SHALL run as a tracked asynchronous operation in the same manner as the original generation,
and SHALL NOT re-upload the documents. The notice SHALL be distinguished from an error by icon and
text, not by colour alone, and SHALL follow the wizard's existing notification and view-state
conventions.

A truncated or empty generation SHALL continue to be presented as an error with no continue action.

#### Scenario: Shortfall offers a continue action

- **WHEN** generation reports a shortfall of 111 questions against an estimate of 168
- **THEN** the wizard shows both counts and the per-section breakdown, states that extraction finished normally, and offers to continue with 111 questions, re-run with more reasoning, or start again

#### Scenario: Continuing after a shortfall

- **WHEN** the user chooses to continue with the produced questions
- **THEN** generation resumes as a tracked operation without re-uploading, and the resulting manifest is reviewed with its findings as usual

#### Scenario: Re-running with more reasoning

- **WHEN** the user chooses to re-run the extraction after a shortfall
- **THEN** a tracked operation repeats the extraction at higher reasoning effort without re-uploading, and its result is either reviewed as usual or presented as a shortfall again

#### Scenario: Truncation offers no continue action

- **WHEN** generation fails because a response was truncated
- **THEN** the wizard shows the error with no action to continue
