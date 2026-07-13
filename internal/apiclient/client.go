// Package apiclient is a thin client for the ragd REST API over its local unix
// socket. The CLI uses it to prefer a running daemon (which owns the backend
// clients and secrets) over constructing backend clients directly. It parses
// the LXD-style sync/async/error envelope and drives async operations to
// completion via the operations/wait endpoint.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/canonical/go-snapctl/env"
)

// socketRelPath is the daemon socket location under $SNAP_COMMON, matching
// api.ResolveSocketConfig. Kept in sync deliberately rather than imported to
// keep the CLI from depending on the server package.
const socketRelPath = "ragd/unix.socket"

// fakeHost is the dummy host used in request URLs; the unix transport ignores
// it and dials the socket instead.
const fakeHost = "http://ragd"

// Client talks to ragd over its unix socket.
type Client struct {
	httpc      *http.Client
	socketPath string
}

// envelope is the union of the sync/async/error response shapes. A caller
// inspects Type to decide which fields are meaningful.
type envelope struct {
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	StatusCode int             `json:"status_code"`
	ErrorCode  int             `json:"error_code"`
	Error      string          `json:"error"`
	Operation  string          `json:"operation"`
	Metadata   json.RawMessage `json:"metadata"`
}

// SocketPath returns the daemon socket path under $SNAP_COMMON, or a temp-dir
// fallback when run outside a snap (mirroring the daemon's own resolution).
func SocketPath() string {
	base := env.SnapCommon()
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, socketRelPath)
}

// New builds a client for the socket at path. It does not contact the daemon;
// call Available to check reachability.
func New(path string) *Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return &Client{
		socketPath: path,
		httpc: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", path)
				},
			},
		},
	}
}

// Detect returns a client when a ragd daemon is reachable on the socket and the
// caller is trusted, or nil otherwise. A missing socket file short-circuits so
// the common no-daemon case stays cheap.
func Detect() *Client {
	path := SocketPath()
	if fi, err := os.Stat(path); err != nil || fi.Mode()&os.ModeSocket == 0 {
		return nil
	}
	c := New(path)
	if !c.trusted() {
		return nil
	}
	return c
}

// SocketPath reports the socket the client dials.
func (c *Client) SocketPath() string { return c.socketPath }

// trusted reports whether GET / succeeds and the caller is authenticated.
func (c *Client) trusted() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	env, err := c.do(ctx, http.MethodGet, "/", "", nil)
	if err != nil {
		return false
	}
	var root struct {
		Auth string `json:"auth"`
	}
	if err := json.Unmarshal(env.Metadata, &root); err != nil {
		return false
	}
	return root.Auth == "trusted"
}

// Sync issues a request expecting a sync response and unmarshals its metadata
// into out (when non-nil). It returns an error for error envelopes.
func (c *Client) Sync(ctx context.Context, method, path string, body any, out any) error {
	env, err := c.doJSON(ctx, method, path, body)
	if err != nil {
		return err
	}
	if env.Type == responseTypeError {
		return apiError(env)
	}
	if out != nil && len(env.Metadata) > 0 {
		return json.Unmarshal(env.Metadata, out)
	}
	return nil
}

// Async issues a request expecting an async response and returns the operation
// URL. It returns an error for error envelopes.
func (c *Client) Async(ctx context.Context, method, path string, body any) (string, error) {
	env, err := c.doJSON(ctx, method, path, body)
	if err != nil {
		return "", err
	}
	if env.Type == responseTypeError {
		return "", apiError(env)
	}
	if env.Operation == "" {
		return "", fmt.Errorf("expected an async operation but got a %q response", env.Type)
	}
	return env.Operation, nil
}

// doJSON encodes body as JSON (when non-nil) and performs the request.
func (c *Client) doJSON(ctx context.Context, method, path string, body any) (*envelope, error) {
	var payload string
	contentType := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request: %w", err)
		}
		payload = string(b)
		contentType = "application/json"
	}
	return c.do(ctx, method, path, payload, map[string]string{"Content-Type": contentType})
}

// do performs an HTTP request over the socket and decodes the envelope.
func (c *Client) do(ctx context.Context, method, path, body string, headers map[string]string) (*envelope, error) {
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, fakeHost+path, rdr)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting ragd: %w", err)
	}
	defer resp.Body.Close()
	return decodeEnvelope(resp.Body)
}

// doRaw performs a request with a caller-supplied body reader and content type
// (used for multipart uploads) and decodes the envelope.
func (c *Client) doRaw(ctx context.Context, method, path string, body io.Reader, contentType string) (*envelope, error) {
	req, err := http.NewRequestWithContext(ctx, method, fakeHost+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting ragd: %w", err)
	}
	defer resp.Body.Close()
	return decodeEnvelope(resp.Body)
}

func decodeEnvelope(r io.Reader) (*envelope, error) {
	var env envelope
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		return nil, fmt.Errorf("decoding ragd response: %w", err)
	}
	return &env, nil
}

// apiError converts an error envelope into a Go error.
func apiError(env *envelope) error {
	if env.Error != "" {
		return fmt.Errorf("ragd: %s", env.Error)
	}
	return fmt.Errorf("ragd returned error code %d", env.ErrorCode)
}

const (
	responseTypeSync  = "sync"
	responseTypeAsync = "async"
	responseTypeError = "error"
)
