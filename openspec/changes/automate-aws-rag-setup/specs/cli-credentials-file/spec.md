# Spec Delta

## Purpose

Lets the rag-cli CLI find its OpenSearch and chat secrets in a per-user, owner-only file when they
are not exported in the shell, so ordinary commands work from a fresh shell without editing shell
startup files, while the `ragd` daemon keeps its environment-only contract.

## ADDED Requirements

### Requirement: Environment first, then the credentials file
For `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD` and `CHAT_API_KEY`, the `rag-cli.rag` CLI SHALL use
the environment value when the variable is set, and otherwise the value from
`$SNAP_USER_COMMON/credentials.json` when that file exists and contains the key. An environment
variable that is set, even to an empty string, SHALL take precedence over the file. The CLI SHALL
read and validate the file only when a credential it needs is not set in the environment; a
command whose required credentials are all supplied through the environment SHALL NOT be affected
by a missing, malformed, or insecure file. For
`CHAT_API_KEY` an empty resolved value SHALL mean "send no API key", as today.

#### Scenario: Fresh shell with a credentials file
- **WHEN** no OpenSearch or chat variables are exported and `credentials.json` holds
  `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD` and `CHAT_API_KEY`
- **THEN** `rag-cli.rag knowledge list` authenticates to OpenSearch and `rag-cli.rag chat` sends
  the API key, both using the file's values

#### Scenario: Exported value wins
- **WHEN** `OPENSEARCH_PASSWORD` is exported and the file holds a different password
- **THEN** the CLI uses the exported password

#### Scenario: Explicitly empty value wins
- **WHEN** `CHAT_API_KEY` is exported as an empty string and the file holds a key
- **THEN** the chat client sends no API key and the file is not consulted for it

#### Scenario: Invalid file ignored when the environment suffices
- **WHEN** `OPENSEARCH_USERNAME` and `OPENSEARCH_PASSWORD` are exported and `credentials.json` is
  malformed or has mode 0644
- **THEN** `rag-cli.rag knowledge list` succeeds without reading the file

### Requirement: Existing behavior without the file
When `$SNAP_USER_COMMON/credentials.json` does not exist, or `SNAP_USER_COMMON` is unset, the CLI
SHALL behave exactly as before this change, including the existing error when an OpenSearch
variable is missing, extended only with a hint naming the credentials file.

#### Scenario: No file, no variables
- **WHEN** neither the variables nor the file exist and the user runs `rag-cli.rag knowledge list`
- **THEN** the command fails reporting that `OPENSEARCH_USERNAME` is not set and naming the
  credentials file as an alternative

### Requirement: File format and protection
The file SHALL be a regular file (not a symlink) owned by the process's effective user, with no
group or other permission bits, containing one JSON object whose keys are a subset of
`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD` and `CHAT_API_KEY` with string values. It SHALL be
read as data. When a needed credential requires the file fallback and the file exists but violates
any rule, the command SHALL fail with an error that names the file, states the violated rule and
how to fix it, and SHALL NOT include any credential value.

#### Scenario: Readable by others
- **WHEN** `credentials.json` has mode 0644 and the OpenSearch variables are not exported
- **THEN** a command needing OpenSearch fails with a message that includes the path and
  `chmod 600`, and no secret value

#### Scenario: Unknown key or malformed JSON
- **WHEN** the file contains `{"OPENSEARCH_PASSWORD": "x", "AWS_SECRET": "y"}` or is not valid JSON,
  and a command needs a credential that is not exported
- **THEN** the command fails naming the file and the unknown key or the parse error position,
  without echoing values

#### Scenario: Symlinked file
- **WHEN** `credentials.json` is a symlink and a command needs a credential that is not exported
- **THEN** the command fails stating that the credentials file must be a regular file

### Requirement: Fallback limited to the CLI
The file fallback SHALL apply only to the `rag-cli.rag` CLI process. The `ragd` daemon SHALL
continue to read these secrets only from its own service environment.

#### Scenario: Daemon ignores the file
- **WHEN** root's `SNAP_USER_COMMON` contains a valid `credentials.json` and `ragd` starts without
  `OPENSEARCH_PASSWORD` in its environment
- **THEN** `ragd` reports the knowledge backend unavailable exactly as before this change
