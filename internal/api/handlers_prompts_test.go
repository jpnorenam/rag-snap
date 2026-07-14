package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// promptRequest issues a request against the prompt endpoints over the unix
// socket (where the test runner authenticates as a trusted peer) and returns the
// status code and decoded envelope.
func promptRequest(t *testing.T, sock, method, path string, body any) (int, map[string]any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, "http://unix"+path, reader)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := dialSocket(sock).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var env map[string]any
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decoding envelope for %s %s: %v; body=%s", method, path, err, raw)
	}
	return resp.StatusCode, env
}

// promptViews decodes the metadata of a prompt list response.
func promptViews(t *testing.T, env map[string]any) []promptView {
	t.Helper()
	raw, err := json.Marshal(env["metadata"])
	if err != nil {
		t.Fatalf("re-encoding metadata: %v", err)
	}
	var views []promptView
	if err := json.Unmarshal(raw, &views); err != nil {
		t.Fatalf("decoding prompt views: %v", err)
	}
	return views
}

// promptOne decodes the metadata of a single-prompt response.
func promptOne(t *testing.T, env map[string]any) promptView {
	t.Helper()
	raw, err := json.Marshal(env["metadata"])
	if err != nil {
		t.Fatalf("re-encoding metadata: %v", err)
	}
	var view promptView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decoding prompt view: %v", err)
	}
	return view
}

// TestPromptsListShape verifies GET /1.0/prompts returns the three templates in
// the CLI's fixed order, each carrying its effective value, its built-in
// default, and the customized flag.
func TestPromptsListShape(t *testing.T) {
	sock, _ := startTestServer(t, testBackends())

	status, env := promptRequest(t, sock, http.MethodGet, "/1.0/prompts", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /1.0/prompts: status = %d, want 200", status)
	}
	if env["type"] != responseTypeSync {
		t.Errorf("type = %v, want %q", env["type"], responseTypeSync)
	}

	views := promptViews(t, env)
	want := []string{"chat_system_prompt", "answer_system_prompt", "source_rules"}
	if len(views) != len(want) {
		t.Fatalf("got %d prompts, want %d", len(views), len(want))
	}
	for i, v := range views {
		if v.Name != want[i] {
			t.Errorf("prompt %d: name = %q, want %q", i, v.Name, want[i])
		}
		if v.Customized {
			t.Errorf("prompt %q: customized = true on a fresh daemon, want false", v.Name)
		}
		if v.Value != v.Default || v.Default == "" {
			t.Errorf("prompt %q: an uncustomized prompt should report value == default (non-empty)", v.Name)
		}
	}
}

// TestPromptUpdateAndReset verifies the customized flag transitions across a
// PUT (customize) and a DELETE (reset), and that the change is visible on a
// subsequent GET.
func TestPromptUpdateAndReset(t *testing.T) {
	sock, _ := startTestServer(t, testBackends())
	const custom = "Answer as a pirate, but stay grounded in the context."

	status, env := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/chat_system_prompt",
		map[string]string{"value": custom})
	if status != http.StatusOK {
		t.Fatalf("PUT: status = %d, want 200", status)
	}
	view := promptOne(t, env)
	if !view.Customized || view.Value != custom {
		t.Fatalf("after PUT: customized = %v, value = %q; want true, %q", view.Customized, view.Value, custom)
	}
	if view.Default != chat.DefaultPrompts().ChatSystemPrompt {
		t.Error("after PUT: the built-in default should still be reported")
	}

	// The customization is visible on a fresh read.
	status, env = promptRequest(t, sock, http.MethodGet, "/1.0/prompts/chat_system_prompt", nil)
	if status != http.StatusOK {
		t.Fatalf("GET one: status = %d, want 200", status)
	}
	if got := promptOne(t, env); got.Value != custom || !got.Customized {
		t.Errorf("GET after PUT: value = %q, customized = %v", got.Value, got.Customized)
	}

	// Reset restores the default.
	status, env = promptRequest(t, sock, http.MethodDelete, "/1.0/prompts/chat_system_prompt", nil)
	if status != http.StatusOK {
		t.Fatalf("DELETE: status = %d, want 200", status)
	}
	view = promptOne(t, env)
	if view.Customized {
		t.Error("after DELETE: customized = true, want false")
	}
	if view.Value != chat.DefaultPrompts().ChatSystemPrompt {
		t.Error("after DELETE: value should be the built-in default")
	}

	// Reset is idempotent.
	if status, _ = promptRequest(t, sock, http.MethodDelete, "/1.0/prompts/chat_system_prompt", nil); status != http.StatusOK {
		t.Errorf("second DELETE: status = %d, want 200 (reset is a no-op)", status)
	}
}

// TestPromptUpdateRejectsEmptyValue verifies an empty value is a 400 rather than
// a silent reset, so clearing the editor cannot discard a customization.
func TestPromptUpdateRejectsEmptyValue(t *testing.T) {
	sock, _ := startTestServer(t, testBackends())

	for _, value := range []string{"", "   "} {
		status, env := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/source_rules",
			map[string]string{"value": value})
		if status != http.StatusBadRequest {
			t.Errorf("PUT %q: status = %d, want 400", value, status)
		}
		if env["type"] != responseTypeError {
			t.Errorf("PUT %q: type = %v, want %q", value, env["type"], responseTypeError)
		}
	}
}

// TestPromptUnknownNameIsNotFound verifies an unaddressable prompt name is a 404
// whose message names the valid prompts.
func TestPromptUnknownNameIsNotFound(t *testing.T) {
	sock, _ := startTestServer(t, testBackends())

	status, env := promptRequest(t, sock, http.MethodGet, "/1.0/prompts/not_a_prompt", nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET unknown: status = %d, want 404", status)
	}
	msg, _ := env["error"].(string)
	for _, name := range []string{"chat_system_prompt", "answer_system_prompt", "source_rules"} {
		if !bytes.Contains([]byte(msg), []byte(name)) {
			t.Errorf("404 message %q should name the valid prompt %q", msg, name)
		}
	}

	if status, _ := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/not_a_prompt",
		map[string]string{"value": "x"}); status != http.StatusNotFound {
		t.Errorf("PUT unknown: status = %d, want 404", status)
	}
}

// TestPromptsRequireAuthOnLoopback verifies the prompt endpoints are gated by the
// same authentication as every other /1.0 resource on the loopback listener.
func TestPromptsRequireAuthOnLoopback(t *testing.T) {
	base, srv := startTestServerWithLoopback(t, testBackends())

	// No token: refused.
	resp, err := http.Get(base + "/1.0/prompts")
	if err != nil {
		t.Fatalf("GET /1.0/prompts: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("unauthenticated GET /1.0/prompts: status = %d, want 403", resp.StatusCode)
	}

	// With the daemon's token: admitted.
	req, err := http.NewRequest(http.MethodGet, base+"/1.0/prompts", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+srv.token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GET /1.0/prompts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authenticated GET /1.0/prompts: status = %d, want 200", resp.StatusCode)
	}
}

// TestCustomChatPromptReachesInference is the regression test for the parity bug
// this change fixes: a prompt customized through the API must actually appear as
// the system message on the inference request the daemon makes for a chat
// session. Before this change the daemon read the *client's* local prompts file
// (unreadable from the service's own $HOME) and silently sent the built-in
// default instead.
//
// The embedding-model key is seeded because a chat session applies its system
// prompt only when retrieval is available (handleChatStart's guard).
func TestCustomChatPromptReachesInference(t *testing.T) {
	systemPrompts := make(chan string, 4)
	inference := capturingInference(t, systemPrompts)

	sock, _ := startTestServerWithConfig(t, map[string]string{
		backendOpenSearch: stubOpenSearch(t),
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	}, []string{"knowledge.model.embedding=stub-embedding-model"})

	const custom = "You are a laconic assistant. Answer in one sentence."
	if status, _ := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/chat_system_prompt",
		map[string]string{"value": custom}); status != http.StatusOK {
		t.Fatalf("PUT chat_system_prompt: status = %d, want 200", status)
	}

	conn, ctx, cancel := startChatSession(t, sock)
	defer cancel()
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := wsjson.Write(ctx, conn, map[string]any{"type": "prompt", "content": "hi"}); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}

	select {
	case got := <-systemPrompts:
		if got != custom {
			t.Errorf("inference request carried system prompt %q, want the stored %q", got, custom)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("inference server never received a chat completion request")
	}
}

// TestChatPromptWithoutRetrieval pins the fallback rule at the wire, on a
// server with no embedding model configured (retrieval unavailable): the
// uncustomized RAG-specific default is swapped for the generic assistant
// prompt, but a *customized* prompt is honoured — user configuration is never
// silently overridden (the bug behind the original "prompt ignored without
// retrieval" behaviour).
func TestChatPromptWithoutRetrieval(t *testing.T) {
	systemPrompts := make(chan string, 4)
	inference := capturingInference(t, systemPrompts)

	// Dead OpenSearch and no knowledge.model.embedding key: retrieval is off.
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	})

	askOnce := func(label, want string) {
		t.Helper()
		conn, ctx, cancel := startChatSession(t, sock)
		defer cancel()
		defer conn.Close(websocket.StatusNormalClosure, "")

		if err := wsjson.Write(ctx, conn, map[string]any{"type": "prompt", "content": "hi"}); err != nil {
			t.Fatalf("%s: writing prompt: %v", label, err)
		}
		select {
		case got := <-systemPrompts:
			if got != want {
				t.Errorf("%s: system prompt = %q, want %q", label, got, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: inference server never received a chat completion request", label)
		}
	}

	// Uncustomized: the RAG-specific default gives way to the generic prompt.
	askOnce("default", "You are a helpful assistant.")

	// Customized: the user's prompt runs even though retrieval is unavailable.
	const custom = "You are a laconic assistant. Answer in one sentence."
	if status, _ := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/chat_system_prompt",
		map[string]string{"value": custom}); status != http.StatusOK {
		t.Fatalf("PUT chat_system_prompt: status = %d, want 200", status)
	}
	askOnce("customized", custom)
}

// stubOpenSearch is the minimum OpenSearch a knowledge client will accept: a
// reachable port and a healthy cluster. It exists only so handleChatStart's
// retrieval guard passes (which is what makes a session apply its system
// prompt); searches against it return no hits, which the chat loop tolerates.
func stubOpenSearch(t *testing.T) string {
	t.Helper()

	// knowledge.NewClient reads the backend credentials from the environment.
	t.Setenv("OPENSEARCH_USERNAME", "stub")
	t.Setenv("OPENSEARCH_PASSWORD", "stub")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/_cluster/health") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cluster_name":    "stub",
				"status":          "green",
				"number_of_nodes": 1,
			})
			return
		}
		// Everything else (searches included): a well-formed empty result.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"took":      1,
			"timed_out": false,
			"hits":      map[string]any{"total": map[string]any{"value": 0}, "hits": []any{}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// capturingInference is stubInference plus capture: it publishes the system
// message of every chat-completion request onto systemPrompts, so a test can
// assert which prompt the daemon actually sent.
func capturingInference(t *testing.T, systemPrompts chan<- string) string {
	t.Helper()
	mux := http.NewServeMux()

	models := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "stub-model", "object": "model"}},
		})
	}
	mux.HandleFunc("/models", models)
	mux.HandleFunc("/v1/models", models)

	completions := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			for _, m := range req.Messages {
				if m.Role == "system" {
					select {
					case systemPrompts <- contentText(m.Content):
					default:
					}
					break
				}
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flush, _ := w.(http.Flusher)
		payload := map[string]any{
			"id":      "chatcmpl-stub",
			"object":  "chat.completion.chunk",
			"model":   "stub-model",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "ok"}}},
		}
		b, _ := json.Marshal(payload)
		_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
		if flush != nil {
			flush.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}
	mux.HandleFunc("/chat/completions", completions)
	mux.HandleFunc("/v1/chat/completions", completions)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// contentText flattens an OpenAI message content field, which is either a plain
// string or an array of typed content parts.
func contentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			if m, ok := part.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	}
	return ""
}

// startChatSession posts POST /1.0/chat and dials the resulting websocket.
func startChatSession(t *testing.T, sock string) (*websocket.Conn, context.Context, context.CancelFunc) {
	t.Helper()

	resp, err := dialSocket(sock).Post("http://unix/1.0/chat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /1.0/chat: status = %d, want 202; body=%s", resp.StatusCode, body)
	}

	var env struct {
		Metadata struct {
			Metadata struct {
				Websocket struct {
					URL    string `json:"url"`
					Secret string `json:"secret"`
				} `json:"websocket"`
			} `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding chat operation: %v", err)
	}
	ws := env.Metadata.Metadata.Websocket

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, dresp, err := websocket.Dial(ctx, "ws://unix"+ws.URL+"?secret="+ws.Secret,
		&websocket.DialOptions{HTTPClient: wsDialer(sock)})
	if err != nil {
		cancel()
		t.Fatalf("dialing chat websocket: %v", err)
	}
	if dresp != nil && dresp.Body != nil {
		_ = dresp.Body.Close()
	}
	return conn, ctx, cancel
}

// TestPromptsSeedChatAndBatch verifies the daemon feeds *stored* prompts into the
// work it starts — the parity bug this change exists to fix. A chat session is
// seeded with the stored chat_system_prompt, a batch run with the stored
// answer_system_prompt, and both keep the values they started with when the
// prompt is edited afterwards.
func TestPromptsSeedChatAndBatch(t *testing.T) {
	sock, srv := startTestServer(t, testBackends())
	const customChat = "Custom chat instruction."
	const customAnswer = "Custom answer instruction."

	if status, _ := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/chat_system_prompt",
		map[string]string{"value": customChat}); status != http.StatusOK {
		t.Fatalf("PUT chat_system_prompt: status = %d", status)
	}
	if status, _ := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/answer_system_prompt",
		map[string]string{"value": customAnswer}); status != http.StatusOK {
		t.Fatalf("PUT answer_system_prompt: status = %d", status)
	}

	// This is what handleChatStart and handleAnswerBatch resolve at start.
	resolved := srv.prompts.resolve()
	if resolved.ChatSystemPrompt != customChat {
		t.Errorf("chat session would use %q, want the stored %q", resolved.ChatSystemPrompt, customChat)
	}
	if resolved.AnswerSystemPrompt != customAnswer {
		t.Errorf("batch run would use %q, want the stored %q", resolved.AnswerSystemPrompt, customAnswer)
	}
	// An untouched prompt still resolves to its built-in default.
	if resolved.SourceRules != chat.DefaultPrompts().SourceRules {
		t.Error("an uncustomized prompt should resolve to its built-in default")
	}

	// Editing a prompt after work has started must not mutate the config that
	// work already captured: resolve() returns a value, not a live view.
	if status, _ := promptRequest(t, sock, http.MethodPut, "/1.0/prompts/chat_system_prompt",
		map[string]string{"value": "Changed mid-session."}); status != http.StatusOK {
		t.Fatalf("second PUT: status = %d", status)
	}
	if resolved.ChatSystemPrompt != customChat {
		t.Errorf("a running session's prompts changed under it: %q", resolved.ChatSystemPrompt)
	}
	// ...while the next session picks the new value up.
	if got := srv.prompts.resolve().ChatSystemPrompt; got != "Changed mid-session." {
		t.Errorf("next session would use %q, want the updated prompt", got)
	}
}
