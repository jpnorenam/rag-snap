package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// stubInference is a minimal OpenAI-compatible server that serves a single
// model and streams a fixed SSE chat completion (with a <think> block followed
// by answer content), so the chat websocket can be exercised without a real
// inference backend.
func stubInference(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	models := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "stub-model", "object": "model"}},
		})
	}
	// The configured inference URL may or may not carry a /v1 path prefix; serve
	// both so the stub works regardless of how the client composes the path.
	mux.HandleFunc("/models", models)
	mux.HandleFunc("/v1/models", models)
	completions := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flush, _ := w.(http.Flusher)
		chunks := []string{"<think>", "reasoning", "</think>", "Hello", " world"}
		for _, c := range chunks {
			payload := map[string]any{
				"id":      "chatcmpl-stub",
				"object":  "chat.completion.chunk",
				"model":   "stub-model",
				"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": c}}},
			}
			b, _ := json.Marshal(payload)
			_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
			if flush != nil {
				flush.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}
	mux.HandleFunc("/chat/completions", completions)
	mux.HandleFunc("/v1/chat/completions", completions)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// wsDialer returns a websocket dial that connects over the daemon's unix socket.
func wsDialer(sock string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
}

// TestChatSessionStreamsTurns starts a chat session, holds two prompts over one
// websocket, and verifies the daemon streams think/token frames terminated by a
// done frame each turn — exercising the server-owned multi-turn session.
func TestChatSessionStreamsTurns(t *testing.T) {
	inference := stubInference(t)
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	// Start the session (no bases: pure chat, no retrieval).
	resp, err := client.Post("http://unix/1.0/chat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /1.0/chat status = %d, want 202; body=%s", resp.StatusCode, body)
	}

	// The async envelope's metadata is the operation view, whose own metadata
	// map carries the chat model and websocket connect details.
	var env struct {
		Metadata struct {
			Metadata struct {
				Model     string `json:"model"`
				Websocket struct {
					URL    string `json:"url"`
					Secret string `json:"secret"`
				} `json:"websocket"`
			} `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding chat op: %v", err)
	}
	meta := env.Metadata.Metadata
	if meta.Websocket.URL == "" || meta.Websocket.Secret == "" {
		t.Fatalf("missing websocket url/secret: %+v", meta)
	}
	if meta.Model != "stub-model" {
		t.Errorf("model = %q, want stub-model", meta.Model)
	}

	// Dial the chat websocket over the unix socket.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws://unix" + meta.Websocket.URL + "?secret=" + meta.Websocket.Secret
	conn, dresp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: wsDialer(sock)})
	if err != nil {
		t.Fatalf("dial chat websocket: %v", err)
	}
	if dresp != nil && dresp.Body != nil {
		_ = dresp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	for turn := 0; turn < 2; turn++ {
		if err := wsjson.Write(ctx, conn, map[string]any{"type": "prompt", "content": "hi"}); err != nil {
			t.Fatalf("turn %d write: %v", turn, err)
		}
		var think, answer strings.Builder
		sawDone := false
		for !sawDone {
			var msg chatServerMessage
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				t.Fatalf("turn %d read: %v", turn, err)
			}
			switch msg.Type {
			case string(chat.TokenThink):
				think.WriteString(msg.Content)
			case string(chat.TokenAnswer):
				answer.WriteString(msg.Content)
			case "done":
				sawDone = true
			case "error":
				t.Fatalf("turn %d error frame: %s", turn, msg.Error)
			}
		}
		if !strings.Contains(think.String(), "reasoning") {
			t.Errorf("turn %d think content = %q, want it to contain reasoning", turn, think.String())
		}
		if !strings.Contains(answer.String(), "Hello world") {
			t.Errorf("turn %d answer content = %q, want it to contain \"Hello world\"", turn, answer.String())
		}
	}
}

// TestChatConnectRejectsBadSecret verifies the websocket connect endpoint
// refuses a wrong secret for a chat operation.
func TestChatConnectRejectsBadSecret(t *testing.T) {
	inference := stubInference(t)
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	resp, err := client.Post("http://unix/1.0/chat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	defer resp.Body.Close()
	var env struct {
		Metadata struct {
			Metadata struct {
				Websocket struct {
					URL string `json:"url"`
				} `json:"websocket"`
			} `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding chat op: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws://unix" + env.Metadata.Metadata.Websocket.URL + "?secret=wrong"
	_, hresp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: wsDialer(sock)})
	if err == nil {
		t.Fatalf("dial with bad secret succeeded, want rejection")
	}
	if hresp != nil {
		if hresp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", hresp.StatusCode)
		}
		_ = hresp.Body.Close()
	}
}

// TestChatRecordsPromptProvenance verifies a session started on a named variant
// records that variant@version as provenance on the saved chat, and that an
// unknown variant fails the start request.
func TestChatRecordsPromptProvenance(t *testing.T) {
	inference := stubInference(t)
	sock, srv := startTestServer(t, chatBackends(inference))
	client := dialSocket(sock)

	// Create and save a chat_system_prompt variant (two versions → head is v2).
	if status, _ := promptRequest(t, sock, http.MethodPost, "/1.0/prompts/chat_system_prompt/variants",
		map[string]string{"name": "pirate", "value": "Arr, v1."}); status != http.StatusOK {
		t.Fatalf("create variant: status = %d", status)
	}
	if status, _ := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/chat_system_prompt/variants/pirate",
		map[string]string{"value": "Arr, v2."}); status != http.StatusOK {
		t.Fatalf("save variant v2: status = %d", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	meta := startChatOp(t, client, `{"prompt":"pirate"}`)
	conn := dialChat(ctx, t, sock, meta)
	defer conn.Close(websocket.StatusNormalClosure, "")

	runTurn(ctx, t, conn)
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "save", "title": "arr"}); err != nil {
		t.Fatalf("save write: %v", err)
	}
	var saved chatServerMessage
	for saved.Type != "saved" {
		if err := wsjson.Read(ctx, conn, &saved); err != nil {
			t.Fatalf("read save ack: %v", err)
		}
		if saved.Type == "error" {
			t.Fatalf("save error: %s", saved.Error)
		}
	}

	list, _ := srv.chats.List("")
	if len(list) != 1 {
		t.Fatalf("expected one saved chat, got %d", len(list))
	}
	if list[0].Prompt != "pirate@2" {
		t.Errorf("saved chat provenance = %q, want pirate@2", list[0].Prompt)
	}

	// An unknown variant fails the start request outright.
	resp, err := client.Post("http://unix/1.0/chat", "application/json", strings.NewReader(`{"prompt":"nope"}`))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown variant start: status = %d, want 404", resp.StatusCode)
	}
}
