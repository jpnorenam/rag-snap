## ADDED Requirements

### Requirement: Build wizard offers an analysis path alongside extraction

The build wizard's upload step SHALL let the user choose how the uploaded document becomes a manifest:
the existing extraction path, or the analysis path that generates a complete manifest. Extraction SHALL
remain the default, so the wizard's established behaviour is what an unchanged interaction produces.

When the daemon does not advertise analysis as available, the analysis choice SHALL be shown as
unavailable with the reason, rather than being hidden or offered and then failing on submit.

The upload step SHALL accept, for the analysis path only, any number of additional context documents
alongside the primary RFP document, listed so the user can remove one before submitting. The primary
document SHALL remain distinguishable from the context documents.

Choosing analysis SHALL replace the wizard's remaining steps with the analysis steps rather than
layering on top of them, so the user is never presented with both a column-selection step and an
interview.

#### Scenario: Extraction remains the default

- **WHEN** a user uploads a document and submits without changing the path
- **THEN** the wizard runs the existing extraction flow unchanged

#### Scenario: Adding context documents

- **WHEN** a user selects the analysis path and adds two context documents
- **THEN** both are listed alongside the primary document, each can be removed before submitting, and the primary document remains identifiable

#### Scenario: Analysis unavailable on this daemon

- **WHEN** the daemon does not advertise analysis as available
- **THEN** the analysis choice is shown as unavailable with the reason, and the extraction path still works

### Requirement: Triage is reviewed before the interview

After the analysis upload completes, the wizard SHALL present the triage before asking the interview
questions: the RFP identity, the structure found, the estimated question count, whether OpenStack is in
scope, whether questions carry several answer fields, and the suggested knowledge bases.

The per-document conveyance report SHALL be shown, so a user can see which documents were read natively
and which were converted, and reconsider before spending a generation call.

The user SHALL be able to abandon the analysis at this point without generating a manifest.

The triage step SHALL render the initial, loading, empty and error states, and SHALL surface an expired
analysis session as a recoverable error that returns the user to the upload step rather than as a dead
end.

#### Scenario: Triage is shown before the interview

- **WHEN** the analysis upload operation completes
- **THEN** the wizard shows the triage and the per-document conveyance report, and has not yet asked the interview questions

#### Scenario: Abandoning after triage

- **WHEN** a user abandons at the triage step
- **THEN** no manifest is generated and no further model call is made

#### Scenario: Expired session at generation

- **WHEN** the analysis session has expired by the time the user submits the interview
- **THEN** the error is surfaced as recoverable and the user is returned to the upload step

### Requirement: Interview is a form conditioned on the triage

The wizard SHALL collect the interview as a form: the target answer length in words, prefilled with the
configured default; whether that length is a hard limit that must never be exceeded or a soft target
that may be slightly exceeded, presented as an explicit choice with neither option preselected by
absence; and the OpenStack flavour.

The OpenStack choice SHALL be presented only when the triage reported that the RFP requires OpenStack.
When the triage reported OpenStack is not in scope, the control SHALL NOT be rendered.

The form SHALL not permit submission with an answer length that is absent, zero, or not a number, and
SHALL say what is wrong at the field rather than only on submit.

Submitting SHALL run generation as a tracked asynchronous operation, showing a running state, in the
same manner as the wizard's existing extraction submit.

#### Scenario: Length prefilled with the configured default

- **WHEN** the interview form is shown
- **THEN** the answer length is prefilled with the configured default and the hard-or-soft choice is presented explicitly

#### Scenario: OpenStack choice conditioned on triage

- **WHEN** the triage reported that the RFP does not require OpenStack
- **THEN** the OpenStack flavour control is not rendered

#### Scenario: Invalid answer length

- **WHEN** a user clears the answer length and attempts to submit
- **THEN** the form reports the problem at the field and does not submit

#### Scenario: Generation runs as a tracked operation

- **WHEN** a user submits the interview
- **THEN** generation runs as a tracked asynchronous operation with a visible running state

### Requirement: Generated manifest is reviewed with its check findings before running

When generation completes, the wizard SHALL present the generated manifest for review together with its
check findings, before offering to run it.

The review SHALL show the question count, the knowledge bases and kapa source groups selected, the
domains with the number of questions each would route, and the generated prompt. The prompt SHALL be
readable in full rather than only as a truncated preview, since it is the artefact most likely to need
an edit.

Check findings SHALL be shown at their two severities, visually distinguished by more than colour
alone. A zero-match domain pattern SHALL read as a warning that may be intentional, not as a failure.

The review SHALL offer the same two actions the wizard's configure step offers — download the manifest
without running it, and run it — and downloading SHALL produce a YAML manifest that the CLI accepts
unchanged. A manifest carrying defects SHALL still be downloadable, so the user can fix it by hand.

Generated questions SHALL be editable before running, in the same manner as extracted candidate
questions, and the check findings shown SHALL correspond to what will actually run.

#### Scenario: Manifest and findings are shown together

- **WHEN** generation completes
- **THEN** the wizard shows the question count, selected knowledge bases and kapa source groups, the domains with per-domain match counts, the full generated prompt, and the check findings

#### Scenario: Warning reads as a warning

- **WHEN** the findings include a domain pattern matching no question
- **THEN** it is presented as a warning that may be intentional, distinguished from a defect by more than colour

#### Scenario: Downloading a generated manifest

- **WHEN** a user downloads the generated manifest
- **THEN** the saved YAML is accepted unchanged by the CLI's `answer batch`

#### Scenario: Editing before running

- **WHEN** a user edits the generated questions and then runs
- **THEN** the run uses the edited questions and the findings shown reflect what runs

#### Scenario: Defective manifest is still downloadable

- **WHEN** the check findings include a defect
- **THEN** the manifest can still be downloaded for hand-editing

### Requirement: A manifest can be checked from the UI without running it

The answer screen SHALL let a user check a manifest they supply — one loaded from a file as the run
flow already accepts — and see its findings, without starting a run.

The findings surface SHALL be the same one the generated-manifest review uses, so a hand-written and a
generated manifest are reported identically.

#### Scenario: Checking a supplied manifest

- **WHEN** a user supplies a manifest file and asks for it to be checked
- **THEN** the findings are shown and no run is started

#### Scenario: Same surface for both origins

- **WHEN** a hand-written manifest and a generated manifest carrying the same defects are checked
- **THEN** the findings are presented identically
