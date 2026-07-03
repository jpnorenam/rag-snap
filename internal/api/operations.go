package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Operation classes, mirroring LXD.
const (
	operationClassTask      = "task"
	operationClassWebsocket = "websocket"
	operationClassToken     = "token"
)

// Operation is a background unit of work the API exposes for polling, waiting,
// and cancellation. It is safe for concurrent use: state mutations take the
// mutex and publish an operation event to the hub.
type Operation struct {
	registry *operations

	mu          sync.Mutex
	id          string
	class       string
	description string
	createdAt   time.Time
	updatedAt   time.Time
	statusCode  int
	resources   map[string][]string
	metadata    map[string]any
	mayCancel   bool
	errMsg      string

	cancel context.CancelFunc
	runCtx context.Context // cancelled on shutdown or a DELETE cancel request
	done   chan struct{}   // closed once the operation reaches a terminal state

	// secret gates the websocket connect endpoint for websocket-class
	// operations (an empty secret means no websocket is attached).
	secret string
	// onConnect runs the interaction when a client dials the operation's
	// websocket; set only for websocket-class operations. It returns when the
	// interaction ends, which drives the operation to its terminal state.
	onConnect func(ctx context.Context, conn *websocket.Conn) error
}

// operationView is the JSON representation of an operation in API responses.
type operationView struct {
	ID          string              `json:"id"`
	Class       string              `json:"class"`
	Description string              `json:"description"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	Status      string              `json:"status"`
	StatusCode  int                 `json:"status_code"`
	Resources   map[string][]string `json:"resources"`
	Metadata    map[string]any      `json:"metadata"`
	MayCancel   bool                `json:"may_cancel"`
	Err         string              `json:"err"`
}

// url returns the operation's canonical API URL.
func (op *Operation) url() string {
	return "/1.0/operations/" + op.id
}

// view renders a snapshot of the operation for an API response.
func (op *Operation) view() operationView {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.viewLocked()
}

func (op *Operation) viewLocked() operationView {
	// Copy maps so callers cannot mutate operation state through the view.
	res := make(map[string][]string, len(op.resources))
	for k, v := range op.resources {
		res[k] = append([]string(nil), v...)
	}
	meta := make(map[string]any, len(op.metadata))
	for k, v := range op.metadata {
		meta[k] = v
	}
	return operationView{
		ID:          op.id,
		Class:       op.class,
		Description: op.description,
		CreatedAt:   op.createdAt,
		UpdatedAt:   op.updatedAt,
		Status:      statusText(op.statusCode),
		StatusCode:  op.statusCode,
		Resources:   res,
		Metadata:    meta,
		MayCancel:   op.mayCancel,
		Err:         op.errMsg,
	}
}

// terminal reports whether a status code is a final state.
func terminal(code int) bool {
	return code >= statusCodeSuccess
}

// UpdateMetadata merges progress fields into the operation metadata and
// publishes an operation event. Used by running work to report progress.
func (op *Operation) UpdateMetadata(fields map[string]any) {
	op.mu.Lock()
	if op.metadata == nil {
		op.metadata = map[string]any{}
	}
	for k, v := range fields {
		op.metadata[k] = v
	}
	op.updatedAt = time.Now()
	op.mu.Unlock()
	op.registry.publish(op)
}

// setStatus moves the operation to a new status code, recording an error
// message for failure, closing the done channel on the first terminal
// transition, and publishing an event.
func (op *Operation) setStatus(code int, errMsg string) {
	op.mu.Lock()
	wasTerminal := terminal(op.statusCode)
	op.statusCode = code
	op.errMsg = errMsg
	op.updatedAt = time.Now()
	if terminal(code) && !wasTerminal {
		close(op.done)
	}
	op.mu.Unlock()
	op.registry.publish(op)
}

// operations is the in-memory registry of live operations plus the events hub
// they publish to. Operations are not persisted; a daemon restart drops them.
type operations struct {
	mu     sync.RWMutex
	byID   map[string]*Operation
	hub    *eventsHub
	parent context.Context
}

func newOperations(parent context.Context, hub *eventsHub) *operations {
	return &operations{
		byID:   map[string]*Operation{},
		hub:    hub,
		parent: parent,
	}
}

// newUUID returns a random RFC-4122 v4 UUID string.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// create registers a new operation in the given class and publishes its
// creation event. The operation starts in the Pending state.
func (r *operations) create(class, description string, resources map[string][]string, mayCancel bool) (*Operation, error) {
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	op := &Operation{
		registry:    r,
		id:          id,
		class:       class,
		description: description,
		createdAt:   now,
		updatedAt:   now,
		statusCode:  statusCodePending,
		resources:   resources,
		metadata:    map[string]any{},
		mayCancel:   mayCancel,
		done:        make(chan struct{}),
	}
	r.mu.Lock()
	r.byID[id] = op
	r.mu.Unlock()
	r.publish(op)
	return op, nil
}

// runTask creates a task-class operation and runs fn on a goroutine. fn
// receives a context cancelled on daemon shutdown or by a DELETE cancel
// request, and the operation so it can report progress. The operation is marked
// Success when fn returns nil, Failure on error, and Cancelled if the context
// was cancelled by a cancel request.
func (r *operations) runTask(description string, resources map[string][]string, mayCancel bool, fn func(ctx context.Context, op *Operation) error) (*Operation, error) {
	op, err := r.create(operationClassTask, description, resources, mayCancel)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(r.parent)
	op.mu.Lock()
	op.cancel = cancel
	op.mu.Unlock()

	op.setStatus(statusCodeRunning, "")
	go func() {
		defer cancel()
		err := fn(ctx, op)
		switch {
		case err != nil && ctx.Err() != nil:
			// Context cancelled: treat as a cooperative cancellation.
			op.setStatus(statusCodeCancelled, "operation cancelled")
		case err != nil:
			op.setStatus(statusCodeFailure, err.Error())
		default:
			op.setStatus(statusCodeSuccess, "")
		}
	}()
	return op, nil
}

// createWebsocket registers a websocket-class operation whose interaction runs
// when a client dials its websocket endpoint with the matching secret. The
// operation starts Running and reaches a terminal state when onConnect returns
// (Success on nil, Cancelled if the context was cancelled, Failure otherwise).
// A one-time secret is generated and returned so the handler can advertise the
// connect URL.
func (r *operations) createWebsocket(description string, resources map[string][]string, onConnect func(ctx context.Context, conn *websocket.Conn) error) (*Operation, error) {
	op, err := r.create(operationClassWebsocket, description, resources, true)
	if err != nil {
		return nil, err
	}

	var sb [16]byte
	if _, err := rand.Read(sb[:]); err != nil {
		return nil, err
	}
	secret := hex.EncodeToString(sb[:])

	ctx, cancel := context.WithCancel(r.parent)
	op.mu.Lock()
	op.cancel = cancel
	op.runCtx = ctx
	op.secret = secret
	op.onConnect = onConnect
	op.mu.Unlock()

	op.setStatus(statusCodeRunning, "")
	return op, nil
}

// runConnection drives a websocket-class operation's interaction over conn and
// records the terminal status. It must be called once, by the websocket connect
// handler, after the secret is validated. The interaction observes the
// operation's context, which is cancelled on shutdown or a DELETE cancel.
func (op *Operation) runConnection(conn *websocket.Conn) {
	op.mu.Lock()
	cancel := op.cancel
	ctx := op.runCtx
	fn := op.onConnect
	op.mu.Unlock()

	if cancel != nil {
		defer cancel()
	}

	err := fn(ctx, conn)
	switch {
	case err != nil && ctx.Err() != nil:
		op.setStatus(statusCodeCancelled, "session cancelled")
	case err != nil:
		op.setStatus(statusCodeFailure, err.Error())
	default:
		op.setStatus(statusCodeSuccess, "")
	}
}

// matchesSecret reports whether secret is the operation's one-time websocket
// secret. A websocket-class operation with no secret never matches.
func (op *Operation) matchesSecret(secret string) bool {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.secret != "" && secret == op.secret
}

// secretValue returns the operation's one-time websocket secret, for the
// handler to advertise in the connect URL.
func (op *Operation) secretValue() string {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.secret
}

// get returns the operation with the given id, or nil.
func (r *operations) get(id string) *Operation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

// list returns a snapshot view of all current operations.
func (r *operations) list() []operationView {
	r.mu.RLock()
	defer r.mu.RUnlock()
	views := make([]operationView, 0, len(r.byID))
	for _, op := range r.byID {
		views = append(views, op.view())
	}
	return views
}

// requestCancel asks an operation to stop. It succeeds only if the operation
// allows cancellation and is not already terminal. The underlying work stops
// cooperatively when it observes its context cancelled.
func (op *Operation) requestCancel() error {
	op.mu.Lock()
	if !op.mayCancel {
		op.mu.Unlock()
		return fmt.Errorf("operation may not be cancelled")
	}
	if terminal(op.statusCode) {
		op.mu.Unlock()
		return fmt.Errorf("operation is already complete")
	}
	cancel := op.cancel
	op.statusCode = statusCodeCancelling
	op.updatedAt = time.Now()
	op.mu.Unlock()
	op.registry.publish(op)
	if cancel != nil {
		cancel()
	}
	return nil
}

// wait blocks until the operation reaches a terminal state, the optional
// timeout elapses, or the request context is cancelled. It returns the current
// view and whether the operation is terminal. A zero timeout waits forever
// (until ctx is done).
func (op *Operation) wait(ctx context.Context, timeout time.Duration) operationView {
	if op.isTerminal() {
		return op.view()
	}
	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-op.done:
	case <-timer:
	case <-ctx.Done():
	}
	return op.view()
}

func (op *Operation) isTerminal() bool {
	op.mu.Lock()
	defer op.mu.Unlock()
	return terminal(op.statusCode)
}

// publish emits an operation lifecycle event to the events hub.
func (r *operations) publish(op *Operation) {
	if r.hub == nil {
		return
	}
	r.hub.broadcast(event{
		Type:      eventTypeOperation,
		Timestamp: time.Now(),
		Metadata:  op.view(),
	})
}
