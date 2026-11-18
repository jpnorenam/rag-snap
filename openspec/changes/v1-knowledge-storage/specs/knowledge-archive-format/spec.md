## Purpose

Define the knowledge-base export archive and the checks import applies, so an archive built on
one machine produces the same search behavior on another and incompatible archives are refused
instead of silently degrading search.

## ADDED Requirements

### Requirement: Manifest version 2

Export SHALL write manifest version `2`. The manifest SHALL record the knowledge base name, the
export time, the source and chunk counts, and the embedding model name and version that produced
the archive's embeddings.

#### Scenario: Exported manifest

- **WHEN** a user exports a base
- **THEN** the archive's manifest has version `2`
- **AND** it records the embedding model name and version

### Requirement: Embedding model compatibility

Import SHALL refuse an archive whose recorded embedding model name or version differs from the
model the local engine uses. The error SHALL name both models.

#### Scenario: Matching model

- **WHEN** a user imports an archive whose embedding model matches the local engine
- **THEN** the import proceeds

#### Scenario: Mismatched model

- **WHEN** a user imports an archive built with a different embedding model version
- **THEN** the import fails before any data is written
- **AND** the error names the archive's model and the local model

### Requirement: Older archives are refused

Import SHALL refuse archives with manifest version `1` or with no version, with an error telling
the user to re-export the base with the current release.

#### Scenario: v0 archive

- **WHEN** a user imports an archive exported by v0 (manifest version `1`)
- **THEN** the import fails before any data is written
- **AND** the error says to re-export the base with the current release

### Requirement: Archives carry knowledge-base settings

The archive SHALL carry the base's settings that travel with it, including its default label,
and import SHALL apply them to the imported base.

#### Scenario: Label travels

- **WHEN** a base with default label `upstream` is exported and imported elsewhere
- **THEN** the imported base's default label is `upstream`
