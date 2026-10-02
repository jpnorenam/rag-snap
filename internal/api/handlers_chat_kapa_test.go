package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// kapaRecorder is a stub kapa.ai retrieval API that records the source-group
// ids each retrieval request asked for.
type kapaRecorder struct {
	mu       sync.Mutex
	requests [][]string
}

func (k *kapaRecorder) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Groups []string `json:"source_group_ids_include"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		k.mu.Lock()
		k.requests = append(k.requests, req.Groups)
		k.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"source_url":"https://docs.example/page","content":"kapa content"}]`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (k *kapaRecorder) calls() [][]string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([][]string(nil), k.requests...)
}

// openChatSession starts a daemon (with the given kapa client) and returns a
// connected chat websocket.
func openChatSession(t *testing.T, kapa *knowledge.KapaClient) (context.Context, *websocket.Conn) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := storage.NewFileConfig(cfgPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	urls := map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     stubInference(t),
		backendTika:       "http://127.0.0.1:1",
	}
	sock, _ := startTestServerWithStore(t, dir, urls, cfg, func(o *Options) { o.Kapa = kapa })

	resp, err := dialSocket(sock).Post("http://unix/1.0/chat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /1.0/chat: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /1.0/chat status = %d; body=%s", resp.StatusCode, body)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, dresp, err := websocket.Dial(ctx, "ws://unix"+ws.URL+"?secret="+ws.Secret,
		&websocket.DialOptions{HTTPClient: wsDialer(sock)})
	if err != nil {
		t.Fatalf("dial chat websocket: %v", err)
	}
	if dresp != nil && dresp.Body != nil {
		_ = dresp.Body.Close()
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return ctx, conn
}

// sendControl writes a control frame and returns the next server frame.
func sendControl(ctx context.Context, t *testing.T, conn *websocket.Conn, msg map[string]any) chatServerMessage {
	t.Helper()
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("writing %v: %v", msg["type"], err)
	}
	var ack chatServerMessage
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("reading ack for %v: %v", msg["type"], err)
	}
	return ack
}

// promptTurn sends a prompt and reads frames until the turn's "done".
func promptTurn(ctx context.Context, t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "prompt", "content": "How does MAAS commission servers?"}); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}
	for {
		var msg chatServerMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("reading turn: %v", err)
		}
		switch msg.Type {
		case "done":
			return
		case "error":
			t.Fatalf("turn error: %s", msg.Error)
		}
	}
}

func TestChatKapaSelectionScopesRetrieval(t *testing.T) {
	rec := &kapaRecorder{}
	ctx, conn := openChatSession(t, knowledge.NewKapaClientAt(rec.serve(t), "proj", "key"))

	// A session starts with nothing selected: no kapa.ai request.
	promptTurn(ctx, t, conn)
	if got := rec.calls(); len(got) != 0 {
		t.Fatalf("kapa.ai queried before any selection: %v", got)
	}

	ack := sendControl(ctx, t, conn, map[string]any{"type": "set-active-kapa-groups", "kapa_groups": []string{"g-maas", "g-lxd"}})
	if ack.Type != "active-kapa-groups" || ack.Error != "" || strings.Join(ack.KapaGroups, ",") != "g-maas,g-lxd" {
		t.Fatalf("ack = %+v, want active-kapa-groups [g-maas g-lxd]", ack)
	}
	promptTurn(ctx, t, conn)

	// Changing the knowledge bases leaves the kapa.ai selection alone.
	if ack := sendControl(ctx, t, conn, map[string]any{"type": "set-active-kbs", "bases": []string{"docs"}}); ack.Type != "active-kbs" {
		t.Fatalf("kbs ack = %+v", ack)
	}
	promptTurn(ctx, t, conn)

	// Changing the selection mid-session applies to the next prompt.
	sendControl(ctx, t, conn, map[string]any{"type": "set-active-kapa-groups", "kapa_groups": []string{"g-juju"}})
	promptTurn(ctx, t, conn)

	// Clearing it stops kapa.ai retrieval.
	if ack := sendControl(ctx, t, conn, map[string]any{"type": "set-active-kapa-groups", "kapa_groups": []string{}}); len(ack.KapaGroups) != 0 {
		t.Fatalf("clear ack = %+v, want no groups", ack)
	}
	promptTurn(ctx, t, conn)

	got := rec.calls()
	want := []string{"g-maas,g-lxd", "g-maas,g-lxd", "g-juju"}
	if len(got) != len(want) {
		t.Fatalf("kapa.ai requests = %v, want %v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], ",") != want[i] {
			t.Errorf("request %d queried groups %v, want %s", i, got[i], want[i])
		}
	}
}

func TestChatKapaSelectionReportedWhenUnconfigured(t *testing.T) {
	ctx, conn := openChatSession(t, nil)

	ack := sendControl(ctx, t, conn, map[string]any{"type": "set-active-kapa-groups", "kapa_groups": []string{"g-maas"}})
	if ack.Type != "active-kapa-groups" {
		t.Fatalf("ack type = %q, want active-kapa-groups", ack.Type)
	}
	if len(ack.KapaGroups) != 0 {
		t.Errorf("ack groups = %v, want none applied", ack.KapaGroups)
	}
	if !strings.Contains(ack.Error, "not configured") {
		t.Errorf("ack error = %q, want it to report kapa.ai is not configured", ack.Error)
	}

	// The session keeps working on local knowledge.
	promptTurn(ctx, t, conn)
}

// TestChatKapaFailureIsAWarning covers a kapa.ai request failing during a turn:
// the daemon reports it as a non-fatal warning frame ahead of the answer, and
// the turn still completes with "done" rather than "error".
func TestChatKapaFailureIsAWarning(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(failing.Close)
	ctx, conn := openChatSession(t, knowledge.NewKapaClientAt(failing.URL, "proj", "key"))

	sendControl(ctx, t, conn, map[string]any{"type": "set-active-kapa-groups", "kapa_groups": []string{"g-maas"}})
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "prompt", "content": "How does MAAS commission servers?"}); err != nil {
		t.Fatalf("writing prompt: %v", err)
	}

	var warnings []string
	sawAnswer := false
	for done := false; !done; {
		var msg chatServerMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("reading turn: %v", err)
		}
		switch msg.Type {
		case "warning":
			if sawAnswer {
				t.Error("warning frame arrived after answer tokens, want it first")
			}
			warnings = append(warnings, msg.Content)
		case "token":
			sawAnswer = true
		case "error":
			t.Fatalf("turn failed with error frame %q, want a warning and a completed turn", msg.Error)
		case "done":
			done = true
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "kapa.ai retrieval failed") {
		t.Errorf("warnings = %q, want one kapa.ai failure warning", warnings)
	}
	if !sawAnswer {
		t.Error("no answer tokens, want the turn to complete on local context")
	}
}

// newKapaChatServer starts a daemon with the given kapa client and returns its
// socket and server (for seeding the saved-chat store directly).
func newKapaChatServer(t *testing.T, kapa *knowledge.KapaClient) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := storage.NewFileConfig(cfgPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	urls := map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     stubInference(t),
		backendTika:       "http://127.0.0.1:1",
	}
	return startTestServerWithStore(t, dir, urls, cfg, func(o *Options) { o.Kapa = kapa })
}

// openKapaChat dials a started session's websocket with a bounded context.
func openKapaChat(t *testing.T, sock string, meta chatStartMeta) (context.Context, *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn := dialChat(ctx, t, sock, meta)
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return ctx, conn
}

func TestChatStartTimeKapaSelection(t *testing.T) {
	rec := &kapaRecorder{}
	sock, _ := newKapaChatServer(t, knowledge.NewKapaClientAt(rec.serve(t), "proj", "key"))

	meta := startChatOp(t, dialSocket(sock), `{"kapa_source_groups": ["g-maas"]}`)
	if strings.Join(meta.KapaGroups, ",") != "g-maas" || meta.KapaUnavailable {
		t.Fatalf("start metadata = groups %v unavailable %v, want [g-maas] applied", meta.KapaGroups, meta.KapaUnavailable)
	}
	ctx, conn := openKapaChat(t, sock, meta)
	promptTurn(ctx, t, conn)
	if got := rec.calls(); len(got) != 1 || strings.Join(got[0], ",") != "g-maas" {
		t.Errorf("kapa.ai requests = %v, want one for [g-maas]", got)
	}
}

func TestChatStartTimeKapaSelectionUnconfigured(t *testing.T) {
	sock, _ := newKapaChatServer(t, nil)
	meta := startChatOp(t, dialSocket(sock), `{"kapa_source_groups": ["g-maas"]}`)
	if !meta.KapaUnavailable || len(meta.KapaGroups) != 0 {
		t.Errorf("start metadata = groups %v unavailable %v, want none applied and reported", meta.KapaGroups, meta.KapaUnavailable)
	}
}

func TestChatSaveResumeKeepsKapaSelection(t *testing.T) {
	rec := &kapaRecorder{}
	sock, srv := newKapaChatServer(t, knowledge.NewKapaClientAt(rec.serve(t), "proj", "key"))

	// Select groups mid-session, hold a turn, then save.
	ctx, conn := openKapaChat(t, sock, startChatOp(t, dialSocket(sock), `{}`))
	sendControl(ctx, t, conn, map[string]any{"type": "set-active-kapa-groups", "kapa_groups": []string{"g-maas", "g-lxd"}})
	promptTurn(ctx, t, conn)
	saved := sendControl(ctx, t, conn, map[string]any{"type": "save", "title": "kapa chat"})
	if saved.Type != "saved" || saved.ChatID == "" {
		t.Fatalf("save ack = %+v, want a saved chat id", saved)
	}
	stored, err := srv.chats.Get(saved.ChatID)
	if err != nil {
		t.Fatalf("reading saved chat: %v", err)
	}
	if strings.Join(stored.KapaGroups, ",") != "g-maas,g-lxd" {
		t.Fatalf("saved kapa groups = %v, want [g-maas g-lxd]", stored.KapaGroups)
	}

	// Resuming restores them, and the next prompt queries them.
	meta := startChatOp(t, dialSocket(sock), `{"resume": "`+saved.ChatID+`"}`)
	if meta.Chat == nil || strings.Join(meta.Chat.KapaGroups, ",") != "g-maas,g-lxd" || meta.Chat.KapaUnavailable {
		t.Fatalf("resume metadata chat = %+v, want [g-maas g-lxd] restored", meta.Chat)
	}
	ctx2, conn2 := openKapaChat(t, sock, meta)
	promptTurn(ctx2, t, conn2)
	got := rec.calls()
	if last := got[len(got)-1]; strings.Join(last, ",") != "g-maas,g-lxd" {
		t.Errorf("resumed session queried %v, want [g-maas g-lxd]", last)
	}
}

func TestChatResumeWithoutKapaField(t *testing.T) {
	sock, srv := newKapaChatServer(t, knowledge.NewKapaClientAt("http://127.0.0.1:1", "proj", "key"))
	// A record saved before kapa groups were persisted.
	old, err := srv.chats.Save(chatstore.Chat{Title: "old", Model: "stub-model",
		Turns: []chatstore.Turn{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}})
	if err != nil {
		t.Fatalf("seeding chat: %v", err)
	}
	meta := startChatOp(t, dialSocket(sock), `{"resume": "`+old.ID+`"}`)
	if meta.Chat == nil || len(meta.Chat.KapaGroups) != 0 || meta.Chat.KapaUnavailable {
		t.Errorf("resume metadata chat = %+v, want no kapa selection and nothing reported", meta.Chat)
	}
}

func TestChatResumeKapaSelectionUnconfigured(t *testing.T) {
	sock, srv := newKapaChatServer(t, nil)
	saved, err := srv.chats.Save(chatstore.Chat{Title: "kapa chat", Model: "stub-model", KapaGroups: []string{"g-maas"},
		Turns: []chatstore.Turn{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}})
	if err != nil {
		t.Fatalf("seeding chat: %v", err)
	}
	meta := startChatOp(t, dialSocket(sock), `{"resume": "`+saved.ID+`"}`)
	if meta.Chat == nil || !meta.Chat.KapaUnavailable || len(meta.Chat.KapaGroups) != 0 {
		t.Errorf("resume metadata chat = %+v, want the saved selection reported as not applied", meta.Chat)
	}
}
