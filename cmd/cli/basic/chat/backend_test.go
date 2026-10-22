package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestResolveAPI(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", APIOpenAI},
		{"openai", APIOpenAI},
		{" anthropic ", APIAnthropic}, // surrounding whitespace is tolerated
	} {
		got, err := ResolveAPI(c.in)
		if err != nil || got != c.want {
			t.Errorf("ResolveAPI(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}

	_, err := ResolveAPI("bedrock")
	if err == nil {
		t.Fatal("ResolveAPI(bedrock) succeeded, want an error")
	}
	for _, want := range []string{ConfAPI, APIOpenAI, APIAnthropic} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if _, err := NewBackend(BackendConfig{API: "bedrock", BaseURL: "http://127.0.0.1:1"}); err == nil {
		t.Error("NewBackend accepted chat.api=bedrock")
	}
}

// capturedRequest is one request a fake inference server received.
type capturedRequest struct {
	Path    string
	Headers http.Header
	Body    map[string]any
}

// fakeServer records every request and answers each with respond.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []capturedRequest
}

func newFakeServer(t *testing.T, respond func(w http.ResponseWriter, body map[string]any)) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, capturedRequest{Path: r.URL.Path, Headers: r.Header.Clone(), Body: body})
		f.mu.Unlock()
		respond(w, body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) captured() []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedRequest(nil), f.requests...)
}

// writeMessageStream answers with a Messages API SSE stream: an optional
// thinking block, an optional text block, and the given stop reason.
func writeMessageStream(w http.ResponseWriter, thinking, text, stopReason, refusalCategory string) {
	w.Header().Set("Content-Type", "text/event-stream")
	event := func(name, data string) { _, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data) }
	quote := func(s string) string { b, _ := json.Marshal(s); return string(b) }

	event("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`)
	index := 0
	if thinking != "" {
		event("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"thinking","thinking":"","signature":""}}`, index))
		event("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"thinking_delta","thinking":%s}}`, index, quote(thinking)))
		event("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
		index++
	}
	if text != "" {
		event("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, index))
		event("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, index, quote(text)))
		event("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index))
	}
	details := "null"
	if refusalCategory != "" {
		details = fmt.Sprintf(`{"type":"refusal","category":%s,"explanation":null}`, quote(refusalCategory))
	}
	event("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%s,"stop_sequence":null,"stop_details":%s},"usage":{"output_tokens":5}}`, quote(stopReason), details))
	event("message_stop", `{"type":"message_stop"}`)
}

// isolateAnthropicEnv sets the chat backend's key and plants ambient Anthropic
// credentials and an endpoint, so a test proves none of them is used.
// ANTHROPIC_API_KEY is left empty on purpose: the SDK's autoloader stops at the
// first credential it finds, so a set API key would mask the auth-token path,
// which is the one that would add a second (Authorization) credential.
func isolateAnthropicEnv(t *testing.T) {
	t.Setenv("CHAT_API_KEY", "bedrock-bearer")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "ambient-auth-token")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("ANTHROPIC_PROFILE", "")
}

func newTestAnthropic(t *testing.T, f *fakeServer) Backend {
	t.Helper()
	b, err := NewBackend(BackendConfig{API: APIAnthropic, BaseURL: f.URL + "/anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAnthropicChatTurn(t *testing.T) {
	isolateAnthropicEnv(t)
	f := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		writeMessageStream(w, "weighing the context", "Hello", "end_turn", "")
	})
	temperature := 0.3

	var kinds []TokenKind
	var deltas []string
	reply, err := newTestAnthropic(t, f).Stream(context.Background(), Request{
		Model:  "anthropic.claude-sonnet-5-5",
		System: "be grounded",
		Messages: []Message{
			{Role: RoleUser, Content: "q1"},
			{Role: RoleAssistant, Content: "a1"},
			{Role: RoleUser, Content: "q2"},
		},
		Temperature: &temperature,
		Purpose:     PurposeChat,
	}, func(kind TokenKind, content string) error {
		kinds = append(kinds, kind)
		deltas = append(deltas, content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Hello" {
		t.Errorf("reply = %q, want only the answer text", reply)
	}
	if len(kinds) != 2 || kinds[0] != TokenThink || kinds[1] != TokenAnswer || deltas[1] != "Hello" {
		t.Errorf("emitted %v %q, want a think token then the answer", kinds, deltas)
	}

	reqs := f.captured()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Path != "/anthropic/v1/messages" {
		t.Errorf("path = %q, want /anthropic/v1/messages", req.Path)
	}
	if got := req.Headers.Get("X-Api-Key"); got != "bedrock-bearer" {
		t.Errorf("x-api-key = %q, want CHAT_API_KEY", got)
	}
	if got := req.Headers.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none (ANTHROPIC_AUTH_TOKEN must be ignored)", got)
	}
	if _, ok := req.Body["temperature"]; ok {
		t.Error("request carries temperature; sampling parameters must never be sent")
	}
	if mt, _ := req.Body["max_tokens"].(float64); mt <= 0 {
		t.Errorf("max_tokens = %v, want a positive ceiling", req.Body["max_tokens"])
	}
	system, _ := req.Body["system"].([]any)
	if len(system) != 1 || system[0].(map[string]any)["text"] != "be grounded" {
		t.Errorf("system = %v, want the prompt as the top-level system block", req.Body["system"])
	}
	messages, _ := req.Body["messages"].([]any)
	roles := make([]string, 0, len(messages))
	for _, m := range messages {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "user,assistant,user" {
		t.Errorf("message roles = %v, want only user/assistant turns", roles)
	}
	thinking, _ := req.Body["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" || thinking["display"] != "summarized" {
		t.Errorf("thinking = %v, want adaptive with a summarized display for chat", req.Body["thinking"])
	}
}

func TestAnthropicPurposeTuning(t *testing.T) {
	isolateAnthropicEnv(t)
	f := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		writeMessageStream(w, "", "ok", "end_turn", "")
	})
	b := newTestAnthropic(t, f)
	for _, p := range []Purpose{PurposeRewrite, PurposeAnswer} {
		if _, err := b.Complete(context.Background(), Request{Model: "m", System: "sys", Messages: []Message{{Role: RoleUser, Content: "x"}}, Purpose: p}); err != nil {
			t.Fatal(err)
		}
	}
	reqs := f.captured()

	rewrite := reqs[0].Body
	if rewrite["max_tokens"] != float64(anthropicRewriteMaxTokens) {
		t.Errorf("rewrite max_tokens = %v, want %d", rewrite["max_tokens"], anthropicRewriteMaxTokens)
	}
	if oc, _ := rewrite["output_config"].(map[string]any); oc["effort"] != "low" {
		t.Errorf("rewrite output_config = %v, want effort low", rewrite["output_config"])
	}

	answer := reqs[1].Body
	if _, ok := answer["thinking"]; ok {
		t.Error("answer request sets thinking; only chat asks for a summarized display")
	}
	if _, ok := answer["output_config"]; ok {
		t.Error("answer request sets output_config; answers use the model's default effort")
	}
	system, _ := answer["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("answer system = %v", answer["system"])
	}
	if cc, _ := system[0].(map[string]any)["cache_control"].(map[string]any); cc["type"] != "ephemeral" {
		t.Errorf("answer system block cache_control = %v, want ephemeral", system[0])
	}
}

func TestAnthropicStopReasons(t *testing.T) {
	isolateAnthropicEnv(t)

	refused := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		writeMessageStream(w, "", "", "refusal", "cyber")
	})
	_, err := newTestAnthropic(t, refused).Complete(context.Background(), Request{Model: "m", Purpose: PurposeAnswer})
	var refusal *RefusalError
	if !errors.As(err, &refusal) || refusal.Category != "cyber" {
		t.Errorf("refusal err = %v, want a RefusalError with category cyber", err)
	}

	exhausted := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		writeMessageStream(w, "thought until the budget ran out", "", "max_tokens", "")
	})
	_, err = newTestAnthropic(t, exhausted).Complete(context.Background(), Request{Model: "m", Purpose: PurposeAnswer})
	var ceiling *OutputCeilingError
	if !errors.As(err, &ceiling) || ceiling.MaxTokens != anthropicAnswerMaxTokens || !strings.Contains(err.Error(), "max_tokens") {
		t.Errorf("exhausted err = %v, want an OutputCeilingError naming max_tokens", err)
	}

	truncated := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		writeMessageStream(w, "", "partial answer", "max_tokens", "")
	})
	if got, err := newTestAnthropic(t, truncated).Complete(context.Background(), Request{Model: "m", Purpose: PurposeAnswer}); err != nil || got != "partial answer" {
		t.Errorf("truncated = %q, %v; want the partial text", got, err)
	}
}

func TestAnthropicPingAndModels(t *testing.T) {
	isolateAnthropicEnv(t)
	healthy := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":"max_tokens","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":16}}`)
	})
	b := newTestAnthropic(t, healthy)
	if err := b.Ping(context.Background(), "m"); err != nil {
		t.Errorf("Ping on a max_tokens stop = %v, want healthy", err)
	}
	if mt := healthy.captured()[0].Body["max_tokens"]; mt != float64(anthropicPingMaxTokens) {
		t.Errorf("ping max_tokens = %v, want %d", mt, anthropicPingMaxTokens)
	}

	forbidden := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"permission_error","message":"no access"}}`)
	})
	err := newTestAnthropic(t, forbidden).Ping(context.Background(), "m")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || !statusErr.Unauthorized() {
		t.Errorf("Ping on 403 = %v, want an unauthorized StatusError", err)
	}

	if _, err := b.ListModels(context.Background()); !errors.Is(err, ErrModelListingUnsupported) {
		t.Errorf("ListModels = %v, want ErrModelListingUnsupported", err)
	}
	_, err = FindModelName(BackendConfig{API: APIAnthropic, BaseURL: healthy.URL})
	if !errors.Is(err, ErrModelListingUnsupported) || !strings.Contains(err.Error(), "chat.model") {
		t.Errorf("FindModelName = %v, want guidance naming chat.model", err)
	}
	if n := len(healthy.captured()); n != 1 {
		t.Errorf("model lookup made %d extra requests, want none", n-1)
	}
}

// The OpenAI backend keeps today's request shape: the system prompt leads the
// messages, temperature is forwarded, and only the rewrite carries a ceiling.
func TestOpenAIRequestShape(t *testing.T) {
	t.Setenv("CHAT_API_KEY", "")
	f := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"c1","object":"chat.completion","created":0,"model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
	})
	b, err := NewBackend(BackendConfig{BaseURL: f.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	temperature := 0.1
	for _, p := range []Purpose{PurposeAnswer, PurposeRewrite} {
		got, err := b.Complete(context.Background(), Request{Model: "m", System: "sys", Messages: []Message{{Role: RoleUser, Content: "x"}}, Temperature: &temperature, Purpose: p})
		if err != nil || got != "done" {
			t.Fatalf("Complete = %q, %v", got, err)
		}
	}
	reqs := f.captured()
	answer := reqs[0]
	if answer.Path != "/v1/chat/completions" {
		t.Errorf("path = %q", answer.Path)
	}
	if answer.Body["temperature"] != 0.1 {
		t.Errorf("temperature = %v, want 0.1 forwarded", answer.Body["temperature"])
	}
	if first := answer.Body["messages"].([]any)[0].(map[string]any); first["role"] != "system" || first["content"] != "sys" {
		t.Errorf("first message = %v, want the system prompt", first)
	}
	if _, ok := answer.Body["max_tokens"]; ok {
		t.Error("answer request carries max_tokens; the OpenAI backend leaves it to the server")
	}
	if reqs[1].Body["max_tokens"] != float64(maxRewriteTokens) {
		t.Errorf("rewrite max_tokens = %v, want %d", reqs[1].Body["max_tokens"], maxRewriteTokens)
	}
}

func TestRunBatchAnthropic(t *testing.T) {
	isolateAnthropicEnv(t)
	f := newFakeServer(t, func(w http.ResponseWriter, body map[string]any) {
		system, _ := json.Marshal(body["system"])
		messages, _ := json.Marshal(body["messages"])
		switch {
		case strings.Contains(string(system), "RAG query optimizer"):
			writeMessageStream(w, "", `{"anchors":[],"expansion":[]}`, "end_turn", "")
		case strings.Contains(string(messages), "REFUSE-ME"):
			writeMessageStream(w, "", "", "refusal", "general_harms")
		default:
			writeMessageStream(w, "", "grounded answer", "end_turn", "")
		}
	})

	orig := batchRetrieve
	t.Cleanup(func() { batchRetrieve = orig })
	batchRetrieve = func(_ *Session, query, _ string, _ bool) string {
		if strings.Contains(query, "NO-CONTEXT") {
			return ""
		}
		return "retrieved chunk"
	}

	manifest := &BatchManifest{
		Model: "anthropic.claude-sonnet-5-5",
		Questions: []BatchQuestion{
			{ID: "1", Question: "What is supported?"},
			{ID: "2", Question: "REFUSE-ME please"},
			{ID: "3", Question: "NO-CONTEXT question"},
		},
	}
	var failed []string
	hooks := BatchHooks{OnError: func(_, _ int, q BatchQuestion, err error) {
		var refusal *RefusalError
		if !errors.As(err, &refusal) || refusal.Category != "general_harms" {
			t.Errorf("question %s failed with %v, want a refusal", q.ID, err)
		}
		failed = append(failed, q.ID)
	}}

	out, err := RunBatch(context.Background(), BackendConfig{API: APIAnthropic, BaseURL: f.URL}, nil, nil, "", manifest, DefaultPrompts(), 0.1, hooks, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(failed, ",") != "2" {
		t.Errorf("failed questions = %v, want only the refused one", failed)
	}
	if len(out.Results) != 2 || out.Results[0].Answer != "grounded answer" || out.Results[1].Answer != noContextAnswer {
		t.Errorf("results = %+v, want the grounded answer then the no-context answer", out.Results)
	}
	if out.Model != "anthropic.claude-sonnet-5-5" {
		t.Errorf("output model = %q", out.Model)
	}
	for _, r := range f.captured() {
		if _, ok := r.Body["temperature"]; ok {
			t.Error("a batch request carries temperature")
		}
		messages, _ := json.Marshal(r.Body["messages"])
		system, _ := json.Marshal(r.Body["system"])
		if strings.Contains(string(messages), "NO-CONTEXT") && !strings.Contains(string(system), "RAG query optimizer") {
			t.Error("the no-context question reached the model; it must short-circuit")
		}
	}
}
