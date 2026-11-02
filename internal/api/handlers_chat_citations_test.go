package api

import (
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
)

// citingOpenSearch is a stub knowledge backend whose searches return one chunk
// of source "pro-guide", whose metadata record links it to a URL.
func citingOpenSearch(t *testing.T) string {
	t.Helper()
	t.Setenv("OPENSEARCH_USERNAME", "stub")
	t.Setenv("OPENSEARCH_PASSWORD", "stub")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/_cluster/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{"cluster_name": "stub", "status": "green", "number_of_nodes": 1})
		case strings.HasSuffix(r.URL.Path, "/_mget"):
			_ = json.NewEncoder(w).Encode(map[string]any{"docs": []map[string]any{{
				"_id":   "pro-guide",
				"found": true,
				"_source": map[string]any{
					"source_id": "pro-guide",
					"title":     "Ubuntu Pro Guide",
					"file_path": "https://ubuntu.com/pro",
				},
			}}})
		case strings.HasSuffix(r.URL.Path, "/_search"):
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": map[string]any{
				"total": map[string]any{"value": 1},
				"hits": []map[string]any{{
					"_index": "rag-snap-context-docs",
					"_id":    "c1",
					"_score": 0.9,
					"_source": map[string]any{
						"content":   "Ubuntu Pro includes FIPS 140-3 modules.",
						"source_id": "pro-guide",
						"label":     "canonical",
					},
				}},
			}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// replyingInference streams reply as the answer to every streaming chat
// completion; non-streaming requests (keyword extraction) get an empty
// completion, which the chat loop tolerates by falling back to the prompt.
func replyingInference(t *testing.T, reply string) string {
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
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-stub", "object": "chat.completion", "model": "stub-model",
				"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": ""}}},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		payload := map[string]any{
			"id": "chatcmpl-stub", "object": "chat.completion.chunk", "model": "stub-model",
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": reply}}},
		}
		b, _ := json.Marshal(payload)
		_, _ = io.WriteString(w, "data: "+string(b)+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}
	mux.HandleFunc("/chat/completions", completions)
	mux.HandleFunc("/v1/chat/completions", completions)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// chatTurnFrames starts a session with body, sends one prompt, and returns the
// frames received up to and including "done".
func chatTurnFrames(t *testing.T, sock, body string) []chatServerMessage {
	t.Helper()
	resp, err := dialSocket(sock).Post("http://unix/1.0/chat", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /1.0/chat: status = %d; body=%s", resp.StatusCode, b)
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
		t.Fatalf("decoding chat op: %v", err)
	}
	ws := env.Metadata.Metadata.Websocket

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, dresp, err := websocket.Dial(ctx, "ws://unix"+ws.URL+"?secret="+ws.Secret,
		&websocket.DialOptions{HTTPClient: wsDialer(sock)})
	if err != nil {
		t.Fatalf("dial chat websocket: %v", err)
	}
	if dresp != nil && dresp.Body != nil {
		_ = dresp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := wsjson.Write(ctx, conn, map[string]any{"type": "prompt", "content": "Does Pro include FIPS?"}); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}
	var frames []chatServerMessage
	for {
		var msg chatServerMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("reading frame: %v", err)
		}
		if msg.Type == "error" {
			t.Fatalf("error frame: %s", msg.Error)
		}
		frames = append(frames, msg)
		if msg.Type == "done" {
			return frames
		}
	}
}

func citingServer(t *testing.T, reply string) string {
	t.Helper()
	sock, _ := startTestServerWithConfig(t, map[string]string{
		backendOpenSearch: citingOpenSearch(t),
		backendOpenAI:     replyingInference(t, reply),
		backendTika:       "http://127.0.0.1:1",
	}, []string{"knowledge.model.embedding=stub-embedding-model"})
	return sock
}

func TestChatSourcesFramePrecedesTokensAndDoneCarriesCited(t *testing.T) {
	sock := citingServer(t, "Yes, Ubuntu Pro includes FIPS 140-3 modules. [1][4]")
	frames := chatTurnFrames(t, sock, `{"bases":["docs"]}`)

	if frames[0].Type != "sources" {
		t.Fatalf("first frame = %q, want sources before any token", frames[0].Type)
	}
	if len(frames[0].Sources) != 1 {
		t.Fatalf("sources frame = %+v, want one retrieved document", frames[0].Sources)
	}
	src := frames[0].Sources[0]
	if src.Marker != 1 || src.Key != "pro-guide" || src.Label != "Ubuntu Pro Guide" || src.Href != "https://ubuntu.com/pro" {
		t.Errorf("retrieved source = %+v, want marker 1 for pro-guide labeled and linked from metadata", src)
	}

	done := frames[len(frames)-1]
	if done.Unsourced {
		t.Fatal("done frame flagged unsourced for a cited answer")
	}
	if len(done.Sources) != 1 || done.Sources[0].Marker != 1 {
		t.Errorf("done sources = %+v, want only the [1] document (invented [4] dropped)", done.Sources)
	}
}

func TestChatUncitedAnswerIsKeptAndFlaggedUnsourced(t *testing.T) {
	sock := citingServer(t, "Yes, Ubuntu Pro includes FIPS 140-3 modules.")
	frames := chatTurnFrames(t, sock, `{"bases":["docs"]}`)

	done := frames[len(frames)-1]
	if !done.Unsourced {
		t.Fatalf("done frame = %+v, want unsourced", done)
	}
	if done.Content != "" {
		t.Errorf("unsourced done frame replaces the answer with %q; the streamed text should stand", done.Content)
	}
	if len(done.Sources) != 0 {
		t.Errorf("unsourced done frame lists sources: %+v", done.Sources)
	}
}

func TestChatWithoutBasesSendsNoSources(t *testing.T) {
	sock := citingServer(t, "Hello! How can I help?")
	frames := chatTurnFrames(t, sock, `{}`)

	for _, f := range frames {
		if f.Type == "sources" {
			t.Fatalf("got a sources frame for a turn without retrieval: %+v", f)
		}
	}
	done := frames[len(frames)-1]
	if done.Unsourced || len(done.Sources) != 0 {
		t.Errorf("done frame = %+v, want a plain, unflagged answer", done)
	}
}
