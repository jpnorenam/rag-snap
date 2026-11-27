## ADDED Requirements

### Requirement: Batch operations are judged when judging is enabled

`POST /1.0/answer/batch` SHALL accept an optional boolean `judge`. When it is absent or true and the daemon can judge, the operation SHALL judge its answers as defined by `answer-judge`. When it is false, the operation SHALL NOT judge.

The operation metadata SHALL carry a `phase` of `answering` or `judging`, together with `judged_done` and `judged_total` counts, so a client can report judging progress after the last answer.

Each published result SHALL carry its `judgment` once the result is judged. The completed results SHALL carry the `judge` summary. Results SHALL be published before their judgments, so a client sees answers as soon as they exist.

A judging failure SHALL NOT fail the operation.

#### Scenario: Judged by default when enabled

- **WHEN** a client posts a manifest without `judge` to a daemon that can judge
- **THEN** the completed results carry a verdict on every result and a `judge` summary

#### Scenario: Client opts out

- **WHEN** a client posts a manifest with `judge: false`
- **THEN** no result carries a verdict and the results carry no `judge` summary

#### Scenario: Judging phase is visible

- **WHEN** every question has been answered but judgments are still outstanding
- **THEN** the operation metadata reports phase `judging` with `judged_done` below `judged_total`

#### Scenario: Judge failure does not fail the operation

- **WHEN** the Gemini API rejects every judge call
- **THEN** the operation completes successfully with every answer
- **AND** each result is marked not judged

### Requirement: Judge availability is advertised as a capability

The daemon SHALL advertise answer judging in its capability list as `answer_judge`. Judging SHALL be advertised only when `judge.enabled` is true, a judge model resolves, and `GEMINI_API_KEY` is present in the daemon's environment.

When judging is enabled but cannot run, a batch operation SHALL carry the reason in its metadata under `judge_unavailable` from the moment it starts. A client can then show that judging will not run, instead of discovering it from the results. A judge turned off by `judge.enabled=false` SHALL carry no such reason, because that is a choice, not a fault.

#### Scenario: Judging available

- **WHEN** a client reads the capability list on a daemon with judging fully configured
- **THEN** answer judging is listed as available

#### Scenario: Judging unconfigured

- **WHEN** a client reads the capability list on a daemon whose service environment has no `GEMINI_API_KEY`
- **THEN** `answer_judge` is not listed
- **AND** a batch operation started on that daemon carries a `judge_unavailable` reason naming the missing key
