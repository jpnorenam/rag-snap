## ADDED Requirements

### Requirement: The build wizard offers a sheet scope for spreadsheet RFPs

When the analysis path is chosen and the RFP document is a spreadsheet, the build wizard's upload step SHALL fetch the document's sheet inventory and list every sheet with its position, name and data-row
count. Each sheet SHALL have a scope control offering Answer, Context only and Exclude, defaulting to Answer.

The control SHALL state what each setting does. It SHALL prevent starting analysis when no sheet is set to
Answer, saying why at the control. The chosen scope SHALL be sent with the documents, and the triage
step SHALL show it beside the instructions the session works to.

For a PDF, DOCX or CSV RFP the sheet list SHALL NOT be shown. The instructions field SHALL remain the way
to scope those documents. Fetching the inventory SHALL render loading and error states. A failed fetch
SHALL leave analysis available without a scope, with the reason shown.

#### Scenario: Scoping a workbook

- **WHEN** a user picks an eight-tab workbook as the RFP on the analysis path
- **THEN** the upload step lists the eight tabs with row counts, each set to Answer
- **AND** after the user sets "Read Me" to Context only and "App B Sizing & BoQ" to Exclude and starts analysis, the triage step shows that scope

#### Scenario: Nothing left to answer

- **WHEN** a user sets every sheet to Context only or Exclude
- **THEN** the control reports that at least one sheet must be answered, and analysis cannot be started

#### Scenario: Non-spreadsheet RFP

- **WHEN** a user picks a PDF as the RFP on the analysis path
- **THEN** no sheet list is shown

#### Scenario: Inventory unavailable

- **WHEN** fetching the sheet inventory fails because the tika service is down
- **THEN** the upload step shows the reason, and analysis can still be started without a scope
