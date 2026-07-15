package apiclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

// ChatControl is a client→server control frame on the chat websocket.
type ChatControl struct {
	Type    string   `json:"type"`
	Content string   `json:"content,omitempty"`
	Bases   []string `json:"bases,omitempty"`
	Title   string   `json:"title,omitempty"`
}

// ChatServerMessage is a server→client frame on the chat websocket. ID and Title
// carry the saved-chat identity on a "saved" frame.
type ChatServerMessage struct {
	Type    string   `json:"type"`
	Content string   `json:"content,omitempty"`
	Bases   []string `json:"bases,omitempty"`
	Error   string   `json:"error,omitempty"`
	ID      string   `json:"id,omitempty"`
	Title   string   `json:"title,omitempty"`
}

// RestoredChat is the transcript and knowledge-base context recovered when a
// session is started by resuming a saved chat.
type RestoredChat struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	Turns        []chatstore.Turn `json:"turns"`
	Bases        []string         `json:"bases"`
	DroppedBases []string         `json:"dropped_bases"`
}

// ChatSession is an open chat websocket plus the resolved model. Restored is set
// only when the session was started from a saved chat via ResumeChat.
type ChatSession struct {
	conn     *websocket.Conn
	Model    string
	Restored *RestoredChat
}

// StartChat creates a chat session via POST /1.0/chat and dials the resulting
// websocket operation, returning a live session. bases/model are optional.
func (c *Client) StartChat(ctx context.Context, model string, bases []string, temperature float64) (*ChatSession, error) {
	body := map[string]any{"temperature": temperature}
	if model != "" {
		body["model"] = model
	}
	if len(bases) > 0 {
		body["bases"] = bases
	}
	return c.openChat(ctx, body)
}

// ResumeChat starts a chat session seeded from the saved chat with id, returning
// the live session with Restored populated from the daemon's session metadata.
func (c *Client) ResumeChat(ctx context.Context, id string) (*ChatSession, error) {
	return c.openChat(ctx, map[string]any{"resume": id})
}

// openChat posts the chat-start body, dials the returned websocket operation, and
// parses the resolved model plus any restored-chat metadata.
func (c *Client) openChat(ctx context.Context, body map[string]any) (*ChatSession, error) {
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

	// The restored transcript/bases are carried in the operation's own metadata
	// under "chat" when this was a resume.
	var restored *RestoredChat
	if raw, ok := op.Metadata["chat"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			var rc RestoredChat
			if json.Unmarshal(b, &rc) == nil {
				restored = &rc
			}
		}
	}

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
	return &ChatSession{conn: conn, Model: resolvedModel, Restored: restored}, nil
}

// Prompt sends a prompt frame.
func (s *ChatSession) Prompt(ctx context.Context, text string) error {
	return wsjson.Write(ctx, s.conn, ChatControl{Type: "prompt", Content: text})
}

// Save sends a save control frame; the daemon replies with a "saved" or "error"
// frame the caller reads next.
func (s *ChatSession) Save(ctx context.Context, title string) error {
	return wsjson.Write(ctx, s.conn, ChatControl{Type: "save", Title: title})
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
