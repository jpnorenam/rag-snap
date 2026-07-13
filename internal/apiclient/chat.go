package apiclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// ChatControl is a client→server control frame on the chat websocket.
type ChatControl struct {
	Type    string   `json:"type"`
	Content string   `json:"content,omitempty"`
	Bases   []string `json:"bases,omitempty"`
}

// ChatServerMessage is a server→client frame on the chat websocket.
type ChatServerMessage struct {
	Type    string   `json:"type"`
	Content string   `json:"content,omitempty"`
	Bases   []string `json:"bases,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// ChatSession is an open chat websocket plus the resolved model.
type ChatSession struct {
	conn  *websocket.Conn
	Model string
}

// StartChat creates a chat session via POST /1.0/chat and dials the resulting
// websocket operation, returning a live session. bases/model are optional.
func (c *Client) StartChat(ctx context.Context, model string, bases []string, temperature float64) (*ChatSession, error) {
	body := map[string]any{}
	if model != "" {
		body["model"] = model
	}
	if len(bases) > 0 {
		body["bases"] = bases
	}
	body["temperature"] = temperature

	env, err := c.doJSON(ctx, "POST", "/1.0/chat", body)
	if err != nil {
		return nil, err
	}
	if env.Type == responseTypeError {
		return nil, apiError(env)
	}

	var op Operation
	if err := unmarshalMeta(env.Metadata, &op); err != nil {
		return nil, fmt.Errorf("decoding chat operation: %w", err)
	}
	wsInfo, _ := op.Metadata["websocket"].(map[string]any)
	wsURL, _ := wsInfo["url"].(string)
	secret, _ := wsInfo["secret"].(string)
	if wsURL == "" || secret == "" {
		return nil, fmt.Errorf("daemon did not return a chat websocket URL")
	}
	resolvedModel := op.MetadataString("model")

	// Dial over the same unix socket. The host in the URL is ignored by the
	// unix dialer; the path carries the operation websocket endpoint.
	dialURL := fakeHost + wsURL + "?secret=" + secret
	conn, resp, err := websocket.Dial(ctx, dialURL, &websocket.DialOptions{HTTPClient: c.httpc})
	if err != nil {
		return nil, fmt.Errorf("dialing chat websocket: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	conn.SetReadLimit(1 << 20)
	return &ChatSession{conn: conn, Model: resolvedModel}, nil
}

// Prompt sends a prompt frame.
func (s *ChatSession) Prompt(ctx context.Context, text string) error {
	return wsjson.Write(ctx, s.conn, ChatControl{Type: "prompt", Content: text})
}

// SetActiveBases sends a set-active-kbs control frame.
func (s *ChatSession) SetActiveBases(ctx context.Context, bases []string) error {
	return wsjson.Write(ctx, s.conn, ChatControl{Type: "set-active-kbs", Bases: bases})
}

// Read reads the next server frame.
func (s *ChatSession) Read(ctx context.Context) (ChatServerMessage, error) {
	var msg ChatServerMessage
	err := wsjson.Read(ctx, s.conn, &msg)
	return msg, err
}

// Close closes the chat websocket cleanly.
func (s *ChatSession) Close() error {
	return s.conn.Close(websocket.StatusNormalClosure, "client closing")
}

// unmarshalMeta decodes operation metadata, tolerating absent metadata.
func unmarshalMeta(raw []byte, op *Operation) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty operation metadata")
	}
	return json.Unmarshal(raw, op)
}
