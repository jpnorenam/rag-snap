package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// TestOperationTaskLifecycle drives a task operation to success and verifies the
// status transitions and that wait returns the terminal state.
func TestOperationTaskLifecycle(t *testing.T) {
	reg := newOperations(context.Background(), newEventsHub())

	release := make(chan struct{})
	op, err := reg.runTask("test op", nil, true, func(_ context.Context, op *Operation) error {
		op.UpdateMetadata(map[string]any{"step": 1})
		<-release
		return nil
	})
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}

	if got := op.view().StatusCode; got != statusCodeRunning {
		t.Errorf("status before release = %d, want %d (running)", got, statusCodeRunning)
	}

	close(release)
	final := op.wait(context.Background(), 2*time.Second)
	if final.StatusCode != statusCodeSuccess {
		t.Errorf("final status = %d, want %d (success)", final.StatusCode, statusCodeSuccess)
	}
	if final.Metadata["step"] != float64(1) && final.Metadata["step"] != 1 {
		t.Errorf("metadata step = %v, want 1", final.Metadata["step"])
	}
}

// TestOperationCancel verifies a cancellable task stops cooperatively and ends
// in the cancelled state.
func TestOperationCancel(t *testing.T) {
	reg := newOperations(context.Background(), newEventsHub())

	started := make(chan struct{})
	op, err := reg.runTask("cancellable", nil, true, func(ctx context.Context, _ *Operation) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	<-started

	if err := op.requestCancel(); err != nil {
		t.Fatalf("requestCancel: %v", err)
	}
	final := op.wait(context.Background(), 2*time.Second)
	if final.StatusCode != statusCodeCancelled {
		t.Errorf("final status = %d, want %d (cancelled)", final.StatusCode, statusCodeCancelled)
	}
}

// TestOperationCancelNonCancellable verifies a non-cancellable operation refuses
// cancellation.
func TestOperationCancelNonCancellable(t *testing.T) {
	reg := newOperations(context.Background(), newEventsHub())
	release := make(chan struct{})
	op, _ := reg.runTask("uncancellable", nil, false, func(_ context.Context, _ *Operation) error {
		<-release
		return nil
	})
	defer close(release)
	if err := op.requestCancel(); err == nil {
		t.Errorf("requestCancel on non-cancellable op should error")
	}
}

// TestEventsWebsocketStreamsOperations connects to GET /1.0/events filtered to
// operation events, launches an operation over the API, and asserts an event
// arrives reflecting it.
func TestEventsWebsocketStreamsOperations(t *testing.T) {
	sock, srv := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, dresp, err := websocket.Dial(ctx, "http://unix/1.0/events?type=operation", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("dial events: %v", err)
	}
	if dresp != nil && dresp.Body != nil {
		_ = dresp.Body.Close()
	}
	defer func() { _ = conn.CloseNow() }()

	// Launch an operation directly on the server's registry (a feature endpoint
	// would do this in a later phase) and assert the event arrives over the
	// subscribed websocket.
	release := make(chan struct{})
	defer close(release)
	if _, err := srv.ops.runTask("evt", nil, false, func(_ context.Context, _ *Operation) error {
		<-release
		return nil
	}); err != nil {
		t.Fatalf("runTask: %v", err)
	}

	var e event
	if err := wsjson.Read(ctx, conn, &e); err != nil {
		t.Fatalf("reading event: %v", err)
	}
	if e.Type != eventTypeOperation {
		t.Errorf("event type = %q, want %q", e.Type, eventTypeOperation)
	}
}

// TestOperationNotFound verifies GET on an unknown operation returns 404.
func TestOperationNotFound(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	resp, err := dialSocket(sock).Get("http://unix/1.0/operations/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
	var env errorResponse
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &env)
	if env.Type != responseTypeError || env.ErrorCode != http.StatusNotFound {
		t.Errorf("error envelope = %+v", env)
	}
}
