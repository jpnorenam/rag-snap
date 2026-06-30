# rest-api-operations Specification

## Purpose

Define the asynchronous operations resource and the events websocket. Any API action that may
exceed roughly one second — model deployment, document ingestion, batch answering, export, and
import — runs as a background operation that clients can poll, wait on, cancel, and watch for
progress. This mirrors LXD's operations/events model.

## Requirements

### Requirement: Long-running actions return background operations

Any API action that may take longer than approximately one second SHALL be executed as a
background operation and SHALL return an asynchronous response referencing the operation URL.
Each operation SHALL be addressable at `/1.0/operations/<uuid>`.

An operation object SHALL carry: a UUID `id`, a `class` (`task`, `websocket`, or `token`), a
`description`, creation and update timestamps, a `status` with a numeric `status_code`, a list
of affected `resources`, an operation-specific `metadata` object, a `may_cancel` flag, and an
`err` string that is empty unless the operation failed.

#### Scenario: A long action creates an operation

- **WHEN** a client triggers a long-running action (for example a batch ingest)
- **THEN** the response is asynchronous and references `/1.0/operations/<uuid>`
- **AND** fetching that URL returns the operation object with its current status and metadata

#### Scenario: Operation status uses numeric codes

- **WHEN** a client inspects an operation
- **THEN** the operation reports a numeric `status_code` that the client uses in preference to the text status
- **AND** running, successful, failed, and cancelled states are distinguishable by code

### Requirement: Listing and inspecting operations

The API SHALL provide `GET /1.0/operations` to list current operations and
`GET /1.0/operations/<uuid>` to retrieve a single operation.

#### Scenario: Listing operations

- **WHEN** a client requests `GET /1.0/operations`
- **THEN** the response lists the current operations

#### Scenario: Inspecting an unknown operation

- **WHEN** a client requests an operation UUID that does not exist
- **THEN** the API returns a `404` error

### Requirement: Waiting for an operation to complete

The API SHALL provide `GET /1.0/operations/<uuid>/wait` that blocks until the operation reaches
a terminal state or an optional timeout elapses, then returns the final operation object. A
timeout that elapses before completion SHALL return the operation in its current (non-terminal)
state rather than an error.

#### Scenario: Waiting until completion

- **WHEN** a client calls `wait` on a running operation with no timeout
- **THEN** the call blocks until the operation succeeds, fails, or is cancelled
- **AND** it then returns the final operation object

#### Scenario: Waiting with a timeout that elapses

- **WHEN** a client calls `wait` with a timeout that elapses before the operation finishes
- **THEN** the call returns the operation in its current state without error

### Requirement: Cancelling an operation

The API SHALL provide `DELETE /1.0/operations/<uuid>` to request cancellation. Cancellation
SHALL succeed only when the operation's `may_cancel` flag is true; the underlying work SHALL be
stopped cooperatively. A cancellation request for an operation that cannot be cancelled SHALL
return an error.

#### Scenario: Cancelling a cancellable operation

- **WHEN** a client issues `DELETE` on an operation whose `may_cancel` is true
- **THEN** the operation is cancelled and its final state reports cancellation
- **AND** the underlying work stops without completing

#### Scenario: Cancelling a non-cancellable operation

- **WHEN** a client issues `DELETE` on an operation whose `may_cancel` is false
- **THEN** the API returns an error and the operation continues

### Requirement: Events websocket for progress notifications

The API SHALL provide `GET /1.0/events` that upgrades to a websocket and streams typed event
messages, including operation lifecycle/progress events and logging events. Clients SHALL be
able to filter the event types they receive. The documentation SHALL advise clients to
subscribe to events before launching an operation to avoid missing early progress.

#### Scenario: Streaming operation progress

- **WHEN** a client connects to `GET /1.0/events` and then launches an operation
- **THEN** the client receives operation events reflecting the operation's progress and completion

#### Scenario: Filtering event types

- **WHEN** a client connects to `GET /1.0/events` requesting only operation events
- **THEN** it receives operation events and does not receive unrelated event types

### Requirement: Operations are not persisted across daemon restarts

In-flight operations SHALL exist only in the running daemon. A daemon restart MAY drop in-flight
operations; the API SHALL NOT be expected to recover them. This trade-off SHALL be documented.

#### Scenario: Operations do not survive a restart

- **WHEN** the daemon restarts while an operation is in flight
- **THEN** that operation is no longer listed after the restart
- **AND** the client is expected to re-issue the action if needed
