package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

// chatStartMeta is the connect + resume metadata carried in the async envelope's
// nested operation metadata.
type chatStartMeta struct {
	Model     string `json:"model"`
	Websocket struct {
		URL    string `json:"url"`
		Secret string `json:"secret"`
	} `json:"websocket"`
	Chat *struct {
		ID           string           `json:"id"`
		Title        string           `json:"title"`
		Turns        []chatstore.Turn `json:"turns"`
		Bases        []string         `json:"bases"`
		DroppedBases []string         `json:"dropped_bases"`
	} `json:"chat"`
}

// startChatOp POSTs to /1.0/chat with the given JSON body and returns the parsed
// connect/resume metadata. It fails the test on any non-202 response.
func startChatOp(t *testing.T, client *http.Client, body string) chatStartMeta {
	t.Helper()
	resp, err := client.Post("http://unix/1.0/chat", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /1.0/chat status = %d, want 202; body=%s", resp.StatusCode, b)
	}
	var env struct {
		Metadata struct {
			Metadata chatStartMeta `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding chat op: %v", err)
	}
	return env.Metadata.Metadata
}

// dialChat dials the chat websocket for the given connect metadata.
func dialChat(ctx context.Context, t *testing.T, sock string, meta chatStartMeta) *websocket.Conn {
	t.Helper()
	wsURL := "ws://unix" + meta.Websocket.URL + "?secret=" + meta.Websocket.Secret
	conn, dresp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: wsDialer(sock)})
	if err != nil {
		t.Fatalf("dial chat websocket: %v", err)
	}
	if dresp != nil && dresp.Body != nil {
		_ = dresp.Body.Close()
	}
	return conn
}

// runTurn sends one prompt and drains frames until the terminal done frame.
func runTurn(ctx context.Context, t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "prompt", "content": "hi"}); err != nil {
		t.Fatalf("prompt write: %v", err)
	}
	for {
		var msg chatServerMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.Type == "done" {
			return
		}
		if msg.Type == "error" {
			t.Fatalf("error frame during turn: %s", msg.Error)
		}
	}
}

func chatBackends(inference string) map[string]string {
	return map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	}
}

// TestChatsCRUD exercises the saved-chats REST resource: list, search, get,
// 404s, and delete.
func TestChatsCRUD(t *testing.T) {
	sock, srv := startTestServer(t, chatBackends("http://127.0.0.1:1"))
	client := dialSocket(sock)

	tika, err := srv.chats.Save(chatstore.Chat{Title: "tika extraction", Turns: []chatstore.Turn{{Role: "user", Content: "how does tika work"}}})
	if err != nil {
		t.Fatalf("seeding chat: %v", err)
	}
	if _, err := srv.chats.Save(chatstore.Chat{Title: "bedrock setup", Turns: []chatstore.Turn{{Role: "user", Content: "configure aws"}}}); err != nil {
		t.Fatalf("seeding chat: %v", err)
	}

	// List returns both summaries (transcript-free).
	var list []chatstore.Summary
	getMeta(t, client, "/1.0/chats", &list)
	if len(list) != 2 {
		t.Fatalf("list = %d chats, want 2", len(list))
	}

	// Search filters by content.
	var filtered []chatstore.Summary
	getMeta(t, client, "/1.0/chats?search=aws", &filtered)
	if len(filtered) != 1 || filtered[0].Title != "bedrock setup" {
		t.Fatalf("search result = %+v, want only bedrock setup", filtered)
	}

	// Get returns the full transcript.
	var full chatstore.Chat
	getMeta(t, client, "/1.0/chats/"+tika.ID, &full)
	if len(full.Turns) != 1 || full.Turns[0].Content != "how does tika work" {
		t.Fatalf("get transcript mismatch: %+v", full.Turns)
	}

	// Unknown id is a 404.
	if code := statusOf(t, client, "GET", "/1.0/chats/deadbeefdeadbeef"); code != http.StatusNotFound {
		t.Fatalf("GET unknown chat status = %d, want 404", code)
	}

	// Delete removes it; deleting again is a 404.
	if code := statusOf(t, client, "DELETE", "/1.0/chats/"+tika.ID); code != http.StatusOK {
		t.Fatalf("DELETE chat status = %d, want 200", code)
	}
	if code := statusOf(t, client, "DELETE", "/1.0/chats/"+tika.ID); code != http.StatusNotFound {
		t.Fatalf("DELETE deleted chat status = %d, want 404", code)
	}
}

// TestChatResumeSeedsTranscript verifies POST /1.0/chat with a resume id carries
// the saved transcript back in the operation metadata.
func TestChatResumeSeedsTranscript(t *testing.T) {
	inference := stubInference(t)
	sock, srv := startTestServer(t, chatBackends(inference))
	client := dialSocket(sock)

	saved, err := srv.chats.Save(chatstore.Chat{
		Title: "prior chat",
		Turns: []chatstore.Turn{
			{Role: "user", Content: "what is the fix"},
			{Role: "assistant", Content: "restart the service"},
		},
	})
	if err != nil {
		t.Fatalf("seeding chat: %v", err)
	}

	meta := startChatOp(t, client, `{"resume":"`+saved.ID+`"}`)
	if meta.Chat == nil {
		t.Fatal("resume metadata missing chat block")
	}
	if meta.Chat.ID != saved.ID || meta.Chat.Title != "prior chat" {
		t.Fatalf("resume chat id/title mismatch: %+v", meta.Chat)
	}
	if len(meta.Chat.Turns) != 2 || meta.Chat.Turns[1].Content != "restart the service" {
		t.Fatalf("resume transcript mismatch: %+v", meta.Chat.Turns)
	}
}

// TestChatResumeUnknownID404 verifies resuming an unknown id fails before any
// session is started.
func TestChatResumeUnknownID404(t *testing.T) {
	sock, _ := startTestServer(t, chatBackends("http://127.0.0.1:1"))
	client := dialSocket(sock)

	resp, err := client.Post("http://unix/1.0/chat", "application/json", strings.NewReader(`{"resume":"deadbeefdeadbeef"}`))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("resume unknown id status = %d, want 404", resp.StatusCode)
	}
}

// TestChatResumeDropsMissingBases verifies a saved base whose index no longer
// exists is dropped from the active set and reported. Retrieval is unavailable in
// the test (no embedding model), so every saved base is dropped.
func TestChatResumeDropsMissingBases(t *testing.T) {
	inference := stubInference(t)
	sock, srv := startTestServer(t, chatBackends(inference))
	client := dialSocket(sock)

	saved, err := srv.chats.Save(chatstore.Chat{
		Title: "with bases",
		Bases: []string{"docs"},
		Turns: []chatstore.Turn{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("seeding chat: %v", err)
	}

	meta := startChatOp(t, client, `{"resume":"`+saved.ID+`"}`)
	if meta.Chat == nil {
		t.Fatal("resume metadata missing chat block")
	}
	if len(meta.Chat.Bases) != 0 {
		t.Fatalf("effective bases = %v, want none (index gone)", meta.Chat.Bases)
	}
	if len(meta.Chat.DroppedBases) != 1 || meta.Chat.DroppedBases[0] != "docs" {
		t.Fatalf("dropped bases = %v, want [docs]", meta.Chat.DroppedBases)
	}
}

// TestChatSaveControlMessage saves a session over the websocket and verifies the
// saved frame and that the chat then appears in the store.
func TestChatSaveControlMessage(t *testing.T) {
	inference := stubInference(t)
	sock, srv := startTestServer(t, chatBackends(inference))
	client := dialSocket(sock)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	meta := startChatOp(t, client, `{}`)
	conn := dialChat(ctx, t, sock, meta)
	defer conn.Close(websocket.StatusNormalClosure, "")

	runTurn(ctx, t, conn)

	if err := wsjson.Write(ctx, conn, map[string]any{"type": "save", "title": "my session"}); err != nil {
		t.Fatalf("save write: %v", err)
	}
	var saved chatServerMessage
	for {
		if err := wsjson.Read(ctx, conn, &saved); err != nil {
			t.Fatalf("read save ack: %v", err)
		}
		if saved.Type == "saved" || saved.Type == "error" {
			break
		}
	}
	if saved.Type != "saved" {
		t.Fatalf("expected saved frame, got %q (%s)", saved.Type, saved.Error)
	}
	if saved.ChatID == "" || saved.Title != "my session" {
		t.Fatalf("saved frame missing id/title: %+v", saved)
	}

	list, err := srv.chats.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != saved.ChatID {
		t.Fatalf("store does not hold the saved chat: %+v", list)
	}
	if list[0].TurnCount == 0 {
		t.Fatal("saved chat has no turns")
	}
}

// TestChatSaveEmptySessionRejected verifies saving before any turn yields an
// error frame and does not create a record, leaving the session usable.
func TestChatSaveEmptySessionRejected(t *testing.T) {
	inference := stubInference(t)
	sock, srv := startTestServer(t, chatBackends(inference))
	client := dialSocket(sock)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	meta := startChatOp(t, client, `{}`)
	conn := dialChat(ctx, t, sock, meta)
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := wsjson.Write(ctx, conn, map[string]any{"type": "save"}); err != nil {
		t.Fatalf("save write: %v", err)
	}
	var msg chatServerMessage
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg.Type != "error" {
		t.Fatalf("expected error frame for empty save, got %q", msg.Type)
	}

	if list, _ := srv.chats.List(""); len(list) != 0 {
		t.Fatalf("empty save created a record: %+v", list)
	}

	// The session still works afterwards.
	runTurn(ctx, t, conn)
}

// TestChatSaveAfterResumeUpdatesOriginal verifies resuming, adding a turn, and
// saving updates the original record in place instead of creating a new one.
func TestChatSaveAfterResumeUpdatesOriginal(t *testing.T) {
	inference := stubInference(t)
	sock, srv := startTestServer(t, chatBackends(inference))
	client := dialSocket(sock)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	original, err := srv.chats.Save(chatstore.Chat{
		Title: "resumable",
		Turns: []chatstore.Turn{{Role: "user", Content: "first"}, {Role: "assistant", Content: "answer"}},
	})
	if err != nil {
		t.Fatalf("seeding chat: %v", err)
	}

	meta := startChatOp(t, client, `{"resume":"`+original.ID+`"}`)
	conn := dialChat(ctx, t, sock, meta)
	defer conn.Close(websocket.StatusNormalClosure, "")

	runTurn(ctx, t, conn)

	if err := wsjson.Write(ctx, conn, map[string]any{"type": "save"}); err != nil {
		t.Fatalf("save write: %v", err)
	}
	var saved chatServerMessage
	for {
		if err := wsjson.Read(ctx, conn, &saved); err != nil {
			t.Fatalf("read save ack: %v", err)
		}
		if saved.Type == "saved" || saved.Type == "error" {
			break
		}
	}
	if saved.Type != "saved" {
		t.Fatalf("expected saved frame, got %q (%s)", saved.Type, saved.Error)
	}
	if saved.ChatID != original.ID {
		t.Fatalf("save after resume changed id: %s -> %s", original.ID, saved.ChatID)
	}

	list, _ := srv.chats.List("")
	if len(list) != 1 {
		t.Fatalf("expected one chat after save-after-resume, got %d", len(list))
	}
	// The resumed chat had 2 turns; one more prompt/answer exchange adds 2 more.
	if list[0].TurnCount < 3 {
		t.Fatalf("expected the added turn to be persisted, turncount=%d", list[0].TurnCount)
	}
}

// getMeta issues a GET expecting a sync response and unmarshals its metadata.
func getMeta(t *testing.T, client *http.Client, path string, out any) {
	t.Helper()
	resp, err := client.Get("http://unix" + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s status = %d, want 200; body=%s", path, resp.StatusCode, b)
	}
	var env struct {
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	if err := json.Unmarshal(env.Metadata, out); err != nil {
		t.Fatalf("unmarshalling %s metadata: %v", path, err)
	}
}

// statusOf issues a request and returns the HTTP status code.
func statusOf(t *testing.T, client *http.Client, method, path string) int {
	t.Helper()
	req, err := http.NewRequest(method, "http://unix"+path, nil)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
