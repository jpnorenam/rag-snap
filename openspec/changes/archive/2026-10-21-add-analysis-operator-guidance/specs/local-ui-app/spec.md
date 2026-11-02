## ADDED Requirements

### Requirement: The build wizard collects analysis instructions

When the analysis path is chosen, the build wizard's upload step SHALL offer an optional, labelled
multi-line field for instructions to the analysis, with an example of the kind of instruction that
helps (granularity, scope, which questions to include). The field SHALL state its length limit and
SHALL report an over-long value at the field rather than only on submit. Instructions SHALL be sent
with the documents, and the triage step SHALL show the instructions the session is working to.

The field SHALL NOT be shown on the extraction path, which makes no model call it could steer.

#### Scenario: Instructions sent with the documents

- **WHEN** a user enters instructions and starts analysis
- **THEN** the instructions are uploaded with the documents and shown on the triage step

#### Scenario: Extraction path shows no instructions field

- **WHEN** the extraction path is chosen
- **THEN** no instructions field is rendered

#### Scenario: Over-long instructions

- **WHEN** a user enters instructions over the limit
- **THEN** the field reports the problem and analysis cannot be started

### Requirement: The generated prompt is editable while the appended rules are not

The generated-manifest review step SHALL present the model-written part of the prompt in an editable,
labelled text area. It SHALL present the rules rag-cli appends below it, read-only and visibly
distinct, stating that they are added by rag-cli and cannot be edited here.

Downloading or running the generated manifest SHALL use the edited model-written part followed by the
unchanged appended rules. An edit SHALL NOT be able to remove, reorder or alter the appended rules.

The step SHALL indicate when the prompt has been edited and SHALL offer to restore the generated
text. Leaving the step with unsaved edits SHALL follow the wizard's existing unsaved-progress guard.

#### Scenario: Editing the generated prompt

- **WHEN** a user edits the model-written part of the prompt and downloads the manifest
- **THEN** the downloaded prompt is the edited part followed by the unchanged appended rules

#### Scenario: Rules cannot be edited

- **WHEN** the review step is shown
- **THEN** the appended rules are displayed read-only, outside the editable area

#### Scenario: Restoring the generated text

- **WHEN** a user who has edited the prompt chooses to restore it
- **THEN** the editable area returns to the generated text and the edited indicator clears
