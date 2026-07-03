package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Operation status codes mirror the daemon's doubled numeric+text scheme: codes
// at or above statusCodeSuccess are terminal.
const (
	statusCodeSuccess   = 200
	statusCodeFailure   = 400
	statusCodeCancelled = 401
)

// Operation is the client view of a daemon operation.
type Operation struct {
	ID         string         `json:"id"`
	Class      string         `json:"class"`
	Status     string         `json:"status"`
	StatusCode int            `json:"status_code"`
	Metadata   map[string]any `json:"metadata"`
	Err        string         `json:"err"`
}

// terminal reports whether the operation has reached a final state.
func (op *Operation) terminal() bool { return op.StatusCode >= statusCodeSuccess }

// WaitOptions controls WaitForOperation's progress reporting.
type WaitOptions struct {
	// OnProgress, when set, is called with the operation each time the daemon
	// returns an updated (non-terminal) view, so callers can render progress.
	OnProgress func(op *Operation)
}

// WaitForOperation long-polls opURL/wait until the operation is terminal or ctx
// is cancelled, then returns the final operation. A non-success terminal state
// is returned as an error. Each poll uses a bounded server-side wait so progress
// metadata can be surfaced between polls.
func (c *Client) WaitForOperation(ctx context.Context, opURL string, opts WaitOptions) (*Operation, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var op Operation
		// timeout=1 makes the daemon return promptly with the current state when
		// the operation is still running, so we can report progress and re-poll.
		if err := c.Sync(ctx, "GET", opURL+"/wait?timeout=1", nil, &op); err != nil {
			return nil, err
		}
		if op.terminal() {
			switch op.StatusCode {
			case statusCodeSuccess:
				return &op, nil
			case statusCodeCancelled:
				return &op, fmt.Errorf("operation cancelled")
			default:
				if op.Err != "" {
					return &op, fmt.Errorf("%s", op.Err)
				}
				return &op, fmt.Errorf("operation failed")
			}
		}
		if opts.OnProgress != nil {
			opts.OnProgress(&op)
		}
		// Small client-side pause to avoid a tight loop if the server returns
		// immediately (e.g. timeout unsupported by an older daemon).
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// MetadataInt reads an integer progress field from an operation's metadata,
// tolerating JSON's float64 decoding. Returns 0 when absent.
func (op *Operation) MetadataInt(key string) int {
	if op.Metadata == nil {
		return 0
	}
	switch v := op.Metadata[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	}
	return 0
}

// MetadataString reads a string field from an operation's metadata. Returns ""
// when absent.
func (op *Operation) MetadataString(key string) string {
	if op.Metadata == nil {
		return ""
	}
	if s, ok := op.Metadata[key].(string); ok {
		return s
	}
	return ""
}
