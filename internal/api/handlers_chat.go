package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// chatIdleTimeout ends a chat session after this period without a client
// message, releasing the server-side session state.
const chatIdleTimeout = 30 * time.Minute

// chatStartRequest is the body of POST /1.0/chat. All fields are optional: the
// model falls back to config/server lookup, and the active bases and
// temperature default to the chat REPL's behaviour.
type chatStartRequest struct {
	Model       string   `json:"model,omitempty"`
	Bases       []string `json:"bases,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

// chatControlMessage is a client→server control frame on the chat websocket.
// Type "prompt" submits a question; type "set-active-kbs" changes the active
// knowledge bases (the API equivalent of the in-REPL /use-knowledge).
type chatControlMessage struct {
	Type    string   `json:"type"`
	Content string   `json:"content,omitempty"`
	Bases   []string `json:"bases,omitempty"`
}

// chatServerMessage is a server→client frame on the chat websocket: streamed
// "token"/"think" content, a terminal "done" per answer, an "active-kbs"
// acknowledgement, or an "error".
type chatServerMessage struct {
	Type    string   `json:"type"`
	Content string   `json:"content,omitempty"`
	Bases   []string `json:"bases,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// defaultChatTemperature matches the chat REPL's default sampling temperature.
const defaultChatTemperature = 0.3

// handleChatStart implements POST /1.0/chat: start a chat session as a
// websocket-class operation. The operation metadata carries the websocket
// connect URL and one-time secret; the client dials it to hold the session.
func (s *Server) handleChatStart(w http.ResponseWriter, r *http.Request) {
	var req chatStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	baseURL := s.clients.openAIURL()
	if baseURL == "" {
		respondError(w, http.StatusInternalServerError, "inference backend URL is not configured")
		return
	}

	// RAG is enabled only when the knowledge backend and an embedding model are
	// both available; otherwise the session answers without retrieval.
	knowledgeClient, _ := s.clients.openSearchClient()
	embeddingModelID := ""
	if knowledgeClient != nil {
		if id, err := s.clients.embeddingModelID(); err == nil {
			embeddingModelID = id
		} else {
			// No embedding model: retrieval is unavailable, so do not wire the
			// knowledge client (mirrors retrieveContext's guard).
			knowledgeClient = nil
		}
	}

	model := req.Model
	if model == "" {
		model = s.clients.chatModelID()
	}
	temperature := defaultChatTemperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}

	prompts := chat.LoadPrompts()
	systemPrompt := "You are a helpful assistant."
	if knowledgeClient != nil {
		systemPrompt = prompts.ChatSystemPrompt
	}

	live, err := chat.NewLiveSession(baseURL, model, knowledgeClient, embeddingModelID, req.Bases, systemPrompt, temperature, s.ctx.Verbose)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "starting chat session: "+err.Error())
		return
	}

	op, err := s.ops.createWebsocket(
		"Chat session",
		map[string][]string{"knowledge": {"/1.0/knowledge"}},
		func(ctx context.Context, conn *websocket.Conn) error {
			return runChatSession(ctx, conn, live)
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Advertise the connect URL and one-time secret in the operation metadata,
	// mirroring LXD's interactive-exec operation.
	op.UpdateMetadata(map[string]any{
		"model": live.Model(),
		"websocket": map[string]any{
			"url":    op.url() + "/websocket",
			"secret": op.secretValue(),
		},
	})
	respondAsync(w, op.url(), op.view())
}

// handleChatConnect implements GET /1.0/operations/{id}/websocket?secret=...:
// upgrade to the chat websocket for a websocket-class operation after checking
// the one-time secret. The interaction runs until the connection closes, an
// idle timeout fires, or the operation is cancelled.
func (s *Server) handleChatConnect(w http.ResponseWriter, r *http.Request) {
	op := s.ops.get(r.PathValue("id"))
	if op == nil {
		respondError(w, http.StatusNotFound, "operation not found")
		return
	}
	if op.class != operationClassWebsocket {
		respondError(w, http.StatusBadRequest, "operation is not a websocket operation")
		return
	}
	if !op.matchesSecret(r.URL.Query().Get("secret")) {
		respondError(w, http.StatusForbidden, "invalid or missing websocket secret")
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the error to the client.
	}
	// Generous read limit so large pasted prompts are not rejected.
	conn.SetReadLimit(1 << 20)

	op.runConnection(conn)
}

// runChatSession runs the multi-turn chat loop over conn: it reads control
// frames, drives one RAG turn per prompt streaming tokens back, and handles
// active-KB changes. It returns when the client disconnects, an idle timeout
// elapses, or ctx is cancelled. The daemon owns the LiveSession for the
// connection's lifetime, so history and active bases persist across turns.
func runChatSession(ctx context.Context, conn *websocket.Conn, live *chat.LiveSession) error {
	defer conn.Close(websocket.StatusNormalClosure, "session closed")

	for {
		readCtx, cancel := context.WithTimeout(ctx, chatIdleTimeout)
		var msg chatControlMessage
		err := wsjson.Read(readCtx, conn, &msg)
		cancel()
		if err != nil {
			// A closed connection or elapsed idle timeout ends the session
			// cleanly; surface a context error so cancellation is recorded.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			status := websocket.CloseStatus(err)
			if status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway || status == -1 {
				return nil
			}
			return nil
		}

		switch strings.TrimSpace(msg.Type) {
		case "prompt":
			text := strings.TrimSpace(msg.Content)
			if text == "" {
				_ = writeChat(ctx, conn, chatServerMessage{Type: "error", Error: "empty prompt"})
				continue
			}
			emit := func(kind chat.TokenKind, content string) error {
				return writeChat(ctx, conn, chatServerMessage{Type: string(kind), Content: content})
			}
			if err := live.Prompt(ctx, text, emit); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				_ = writeChat(ctx, conn, chatServerMessage{Type: "error", Error: err.Error()})
				continue
			}
			if err := writeChat(ctx, conn, chatServerMessage{Type: "done"}); err != nil {
				return nil
			}

		case "set-active-kbs":
			live.SetActiveBases(msg.Bases)
			if err := writeChat(ctx, conn, chatServerMessage{Type: "active-kbs", Bases: live.ActiveBases()}); err != nil {
				return nil
			}

		default:
			_ = writeChat(ctx, conn, chatServerMessage{Type: "error", Error: fmt.Sprintf("unknown control message type %q", msg.Type)})
		}
	}
}

// writeChat sends a server frame with a bounded write deadline so a stuck
// client cannot block the session goroutine indefinitely.
func writeChat(ctx context.Context, conn *websocket.Conn, msg chatServerMessage) error {
	writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return wsjson.Write(writeCtx, conn, msg)
}
