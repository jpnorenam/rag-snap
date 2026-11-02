## MODIFIED Requirements

### Requirement: Analysis is configured by config keys and authenticated by environment

The analysis API key SHALL be supplied by a single Gemini-specific environment variable, `GEMINI_API_KEY`,
and SHALL NOT be stored in, read from, or written to the config layer. It SHALL NOT fall back to the
variable the chat backend uses, or to any other variable, because the chat backend's credential belongs to
a different provider and would be disclosed to Google. It SHALL be read explicitly, with no reliance on
any client library's own environment auto-detection. It SHALL NOT appear in log or verbose output, in an
error message, or in the emitted manifest.

Whether analysis is available, the model id, and the reasoning effort used for each model call SHALL be
configuration, seeded as `package` keys so that a `user` set can override each of them. The model id
SHALL be configurable rather than compiled in, because the published identifier of a Gemini model is
versioned and the Pro tier ships under a preview identifier.

The analysis endpoint SHALL NOT be configuration. Analysis SHALL call only the Gemini API, and SHALL NOT
default to, or fall back to, the chat backend's endpoint.

Reasoning effort SHALL be configurable separately for the mechanical extraction call and the synthesis
call. A level the Gemini API cannot honour for any current model — disabling reasoning entirely — SHALL
be rejected when configuration is loaded, naming the key, rather than being sent to the provider.

The interview defaults — the answer length in words and whether length is enforced as a hard limit or
a soft target — SHALL also be `package` configuration, so a team can change the starting point of the
interview without changing code. The default answer length SHALL be 100 words.

When analysis is requested but `GEMINI_API_KEY` is absent, the request SHALL fail naming that variable,
and SHALL NOT proceed without it. When the absent key coincides with the retired `ANALYSIS_API_KEY`
variable being set, the failure SHALL say that the variable is no longer read and must be renamed.

When the provider rejects the key, the failure SHALL say the key was rejected and name `GEMINI_API_KEY`,
whichever HTTP status the provider uses to report it.

When the configured model id is unknown to the Gemini API, the failure SHALL name the model id and the
config key that set it. When the id carries a gateway namespace prefix, the failure SHALL say that
direct Gemini model ids are unprefixed.

Analysis SHALL NOT silently degrade when it cannot run: it SHALL NOT fall back to answering the
questions, and it SHALL NOT quietly substitute the pattern-based extraction path for the operator's
request.

#### Scenario: Missing API key

- **WHEN** analysis is requested with `GEMINI_API_KEY` absent from the environment
- **THEN** it fails naming `GEMINI_API_KEY`, and no model call is attempted

#### Scenario: Chat backend key is never used for analysis

- **WHEN** analysis is requested with `GEMINI_API_KEY` absent and the chat backend's key present
- **THEN** it fails as for a missing key, and the chat backend's key is not sent to any analysis request

#### Scenario: Retired key variable

- **WHEN** analysis is requested with `GEMINI_API_KEY` absent and `ANALYSIS_API_KEY` set
- **THEN** it fails saying `ANALYSIS_API_KEY` is no longer read and must be renamed to `GEMINI_API_KEY`

#### Scenario: Key is never disclosed

- **WHEN** analysis runs with verbose output enabled, and when analysis fails
- **THEN** the API key appears in neither the verbose output, nor the error message, nor the emitted manifest

#### Scenario: Rejected key

- **WHEN** the Gemini API rejects the key
- **THEN** the failure states that the key was rejected and names `GEMINI_API_KEY`, without including the key

#### Scenario: Gateway-prefixed model id

- **WHEN** the model id is set to an id carrying a gateway namespace such as `google/gemini-3.1-pro-preview`
- **THEN** the failure names the model id and the config key, and says that direct Gemini model ids are unprefixed

#### Scenario: Reasoning disabled in configuration

- **WHEN** a reasoning-effort key is set to disable reasoning entirely
- **THEN** loading configuration fails naming that key, and no model call is attempted

#### Scenario: Model id is overridden on the user layer

- **WHEN** an operator sets the model id on the `user` layer
- **THEN** analysis calls that model instead of the packaged default

## ADDED Requirements

### Requirement: Analysis traffic goes only to the Gemini API and is not retained by it

Analysis SHALL send document content only to Google's Gemini API, authenticated by the operator's own
key, with no gateway or other intermediary in the request path.

The destination, the backend and the credential SHALL be fixed by rag-cli rather than resolved from the
host environment, so that no environment variable honoured by a client library can redirect the request
to another host, select another Google backend, or substitute another credential.

Each analysis request SHALL be self-contained: documents SHALL be conveyed inline in the request, and
analysis SHALL NOT use any provider facility that stores request content beyond the call, such as a file
upload store or server-side conversation state.

When the provider blocks a prompt or a response on safety or policy grounds, analysis SHALL fail naming
the call and the block reason the provider reported, and SHALL NOT produce a manifest. A blocked response
SHALL NOT be reported as a truncation or as a malformed response.

The operator-facing conveyance report SHALL name the Gemini API and the model called.

#### Scenario: Ambient redirect is ignored

- **WHEN** analysis runs on a host where a client library's base-URL, backend-selection or key environment variables are set to other values
- **THEN** the request still goes to the Gemini API with the key from `GEMINI_API_KEY`

#### Scenario: Documents are conveyed inline

- **WHEN** a PDF is analysed
- **THEN** its bytes travel inside the generation request, and no separate upload is made to the provider

#### Scenario: Blocked by the provider

- **WHEN** the provider blocks a triage or generation call on safety or policy grounds
- **THEN** analysis fails naming the call and the reported block reason, and no manifest is written

#### Scenario: Conveyance report names the destination

- **WHEN** documents are conveyed for analysis
- **THEN** the report names the Gemini API and the model id, not a configurable URL
