package analysis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCompletion is the subset of a Gemini generateContent response the client reads.
// Recording responses in this shape keeps the tests offline while still exercising the
// real SDK, the real request construction and the real response checks.
type fakeCompletion struct {
	content      string
	finishReason string // a Gemini finish reason; empty means STOP unless omitFinish
	omitFinish   bool   // send no finish reason at all
	thought      string // a thought part sent before the answer, which must be ignored
	status       int
	errBody      string
	noChoices    bool   // no candidates
	promptBlock  string // with noChoices: the promptFeedback block reason

	// thoughtTokens and answerTokens are the usage counts reported; zero means the
	// defaults of 10 and 20.
	thoughtTokens int
	answerTokens  int
}

// fakeRequest is what the fake server recorded about one call.
type fakeRequest struct {
	path   string
	apiKey string
	body   map[string]any
}

// fakeServer serves a queue of recorded responses and records the requests it received,
// so a test can assert on what was actually sent.
type fakeServer struct {
	*httptest.Server
	responses []fakeCompletion
	calls     int
	requests  []fakeRequest
}

func newFakeServer(t *testing.T, responses ...fakeCompletion) *fakeServer {
	t.Helper()
	fs := &fakeServer{responses: responses}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fs.requests = append(fs.requests, fakeRequest{
			path: r.URL.Path, apiKey: r.Header.Get("x-goog-api-key"), body: body,
		})
		w.Header().Set("Content-Type", "application/json")

		if fs.calls >= len(fs.responses) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":500,"message":"no recorded response","status":"INTERNAL"}}`))
			return
		}
		resp := fs.responses[fs.calls]
		fs.calls++

		if resp.status != 0 && resp.status != http.StatusOK {
			w.WriteHeader(resp.status)
			body := resp.errBody
			if body == "" {
				body = fmt.Sprintf(`{"error":{"code":%d,"message":"failure","status":"UNKNOWN"}}`, resp.status)
			}
			_, _ = w.Write([]byte(body))
			return
		}

		out := map[string]any{"modelVersion": "test-model"}
		if resp.noChoices {
			if resp.promptBlock != "" {
				out["promptFeedback"] = map[string]any{"blockReason": resp.promptBlock}
			}
		} else {
			var parts []any
			if resp.thought != "" {
				parts = append(parts, map[string]any{"text": resp.thought, "thought": true})
			}
			if resp.content != "" {
				parts = append(parts, map[string]any{"text": resp.content})
			}
			candidate := map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}}
			if !resp.omitFinish {
				finish := resp.finishReason
				if finish == "" {
					finish = "STOP"
				}
				candidate["finishReason"] = finish
			}
			out["candidates"] = []any{candidate}
			thoughts, answer := resp.thoughtTokens, resp.answerTokens
			if thoughts == 0 && answer == 0 {
				thoughts, answer = 10, 20
			}
			out["usageMetadata"] = map[string]any{"thoughtsTokenCount": thoughts, "candidatesTokenCount": answer}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(fs.Close)
	return fs
}

// rawBody renders a recorded request body for substring assertions.
func (r fakeRequest) rawBody() string {
	raw, _ := json.Marshal(r.body)
	return string(raw)
}

// generationConfig returns the recorded request's generationConfig object.
func (r fakeRequest) generationConfig(t *testing.T) map[string]any {
	t.Helper()
	cfg, ok := r.body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("request has no generationConfig: %s", truncate(r.rawBody(), 400))
	}
	return cfg
}

const testKey = "test-key-do-not-log"

// testSettings are the settings most tests use.
func testSettings() Settings {
	return Settings{
		Enabled:            true,
		Model:              "test-model",
		AnswerWords:        100,
		AnswerLimit:        LengthSoft,
		ReasoningQuestions: EffortLow,
		ReasoningStrategy:  EffortHigh,
	}
}

// testClient builds a client pointed at a fake server, with a key in the environment.
func testClient(t *testing.T, fs *fakeServer) *Client {
	t.Helper()
	return testClientWith(t, fs, testSettings())
}

func testClientWith(t *testing.T, fs *fakeServer, s Settings) *Client {
	t.Helper()
	t.Setenv(envGeminiKey, testKey)
	c, err := newClient(s, fs.URL, false)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func testCatalogue() *Catalogue {
	return &Catalogue{
		KnowledgeBases: []KnowledgeBaseEntry{{Name: "vendor-rfp", Docs: "120"}, {Name: "platform-docs", Docs: "900"}},
		KapaGroups:     []KapaGroupEntry{{ID: "aaaa-bbbb", Name: "Upstream docs"}},
	}
}

func testConveyance(t *testing.T) *Conveyance {
	t.Helper()
	c := NewConverter("")
	path := writeFile(t, "matrix.csv", complianceCSV)
	conv, err := c.ConveyAll(path, nil)
	if err != nil {
		t.Fatalf("ConveyAll: %v", err)
	}
	return conv
}

const okTriage = `{"estimated_questions":5,"openstack_in_scope":false,"multi_field_answers":false}`

func TestNotConfiguredNamesEveryMissingPrerequisite(t *testing.T) {
	t.Setenv(envGeminiKey, "")
	t.Setenv(envRetiredKey, "")

	err := Validate(Settings{}) // disabled, no model, no key
	if err == nil {
		t.Fatal("expected a not-configured error")
	}
	if !IsNotConfigured(err) {
		t.Errorf("error should be a NotConfiguredError, got %T", err)
	}
	// All three in one pass, so an operator does not discover them one at a time.
	for _, want := range []string{ConfEnabled, ConfModel, envGeminiKey} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestNotConfiguredErrorNeverContainsTheKey(t *testing.T) {
	const secret = "sk-super-secret-value"
	t.Setenv(envGeminiKey, secret)

	// Disabled, so it still fails — but the message must not echo the key.
	err := Validate(Settings{Model: "m"})
	if err == nil {
		t.Fatal("expected an error while analysis is disabled")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the API key leaked into the error message: %v", err)
	}
}

func TestChatBackendKeyIsNeverUsedForAnalysis(t *testing.T) {
	// The chat backend is Bedrock or a local server, so its key could only ever be a
	// credential for the wrong provider. Analysis must refuse to run, not borrow it.
	fs := newFakeServer(t, fakeCompletion{content: okTriage})
	t.Setenv(envGeminiKey, "")
	t.Setenv("CHAT_API_KEY", "bedrock-key-must-not-reach-google")

	_, err := newClient(testSettings(), fs.URL, false)
	if !IsNotConfigured(err) {
		t.Fatalf("expected a not-configured error, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), envGeminiKey) {
		t.Errorf("error should name %s, got: %v", envGeminiKey, err)
	}
	if len(fs.requests) != 0 {
		t.Errorf("no request may be sent without a Gemini key, got %d", len(fs.requests))
	}
}

func TestRetiredKeyVariableGetsARenameHint(t *testing.T) {
	t.Setenv(envGeminiKey, "")
	t.Setenv(envRetiredKey, "a-key-under-the-old-name")

	err := Validate(testSettings())
	if err == nil {
		t.Fatal("the retired variable must not be read")
	}
	msg := err.Error()
	if !strings.Contains(msg, envRetiredKey) || !strings.Contains(msg, "rename") {
		t.Errorf("error should tell the operator to rename %s, got: %v", envRetiredKey, msg)
	}
	if strings.Contains(msg, "a-key-under-the-old-name") {
		t.Errorf("the key value leaked into the error: %v", msg)
	}
}

func TestCompleteSendsCeilingSchemaAndEffort(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: okTriage})
	c := testClient(t, fs)

	if _, err := c.Triage(context.Background(), testConveyance(t), testCatalogue()); err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if len(fs.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(fs.requests))
	}
	req := fs.requests[0]
	if !strings.HasSuffix(req.path, "/models/test-model:generateContent") {
		t.Errorf("request path = %q, want the generateContent method of the configured model", req.path)
	}
	if _, ok := req.body["systemInstruction"]; !ok {
		t.Error("the grounding must travel as the system instruction")
	}

	cfg := req.generationConfig(t)
	// The output ceiling is set explicitly, never left to a provider default: a
	// default sized for ordinary replies truncates the answer without reducing the
	// reasoning that displaced it. Triage has its own, smaller one.
	if got, _ := cfg["maxOutputTokens"].(float64); int(got) != triageMaxOutputTokens {
		t.Errorf("maxOutputTokens = %v, want the triage ceiling %d", cfg["maxOutputTokens"], triageMaxOutputTokens)
	}
	if cfg["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v, want application/json", cfg["responseMimeType"])
	}
	if _, ok := cfg["responseJsonSchema"].(map[string]any); !ok {
		t.Errorf("responseJsonSchema missing: %v", cfg["responseJsonSchema"])
	}
	thinking, _ := cfg["thinkingConfig"].(map[string]any)
	if thinking["thinkingLevel"] != "HIGH" {
		t.Errorf("thinkingLevel = %v, want HIGH (the strategy effort drives triage)", thinking["thinkingLevel"])
	}
}

func TestCompleteOmitsThinkingWhenUnset(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: okTriage})
	c := testClientWith(t, fs, Settings{Enabled: true, Model: "m"})

	if _, err := c.Triage(context.Background(), testConveyance(t), testCatalogue()); err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if _, present := fs.requests[0].generationConfig(t)["thinkingConfig"]; present {
		t.Error("thinkingConfig must be omitted when no effort is configured, leaving the model's default")
	}
}

func TestSchemasArriveVerbatim(t *testing.T) {
	// Gemini accepts a narrower JSON Schema subset than the full specification, so a
	// schema mangled in transit would be rejected — or worse, accepted as something
	// else. What leaves rag-cli must be exactly what the package defines.
	for name, schema := range map[string]map[string]any{
		"rfp_triage":    triageSchema(),
		"rfp_questions": questionsSchema(),
		"rfp_strategy":  strategySchema(),
	} {
		t.Run(name, func(t *testing.T) {
			fs := newFakeServer(t, fakeCompletion{content: `{}`})
			c := testClient(t, fs)
			if _, _, err := c.complete(context.Background(), name, "system", []part{textPart("x")}, name, schema, EffortLow, maxOutputTokens); err != nil {
				t.Fatalf("complete: %v", err)
			}
			want, _ := json.Marshal(schema)
			got, _ := json.Marshal(fs.requests[0].generationConfig(t)["responseJsonSchema"])
			if string(got) != string(want) {
				t.Errorf("schema changed in transit:\n got: %s\nwant: %s", truncate(string(got), 300), truncate(string(want), 300))
			}
		})
	}
}

func TestAmbientEnvironmentCannotRedirectOrReauthenticate(t *testing.T) {
	// Each of these is honoured by the SDK when the client leaves the matching field
	// unset. Pinned explicitly, none of them may move confidential RFP content to
	// another host, another Google backend, or another credential.
	fs := newFakeServer(t, fakeCompletion{content: okTriage})
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "http://127.0.0.1:1/")
	t.Setenv("GOOGLE_VERTEX_BASE_URL", "http://127.0.0.1:1/")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	t.Setenv("GOOGLE_GENAI_USE_ENTERPRISE", "true")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "someone-elses-project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "us-central1")
	t.Setenv("GOOGLE_API_KEY", "hostile-key")
	c := testClient(t, fs)

	if _, err := c.Triage(context.Background(), testConveyance(t), testCatalogue()); err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if len(fs.requests) != 1 {
		t.Fatalf("the request should have reached the pinned destination, got %d request(s)", len(fs.requests))
	}
	if got := fs.requests[0].apiKey; got != testKey {
		t.Errorf("x-goog-api-key = %q, want the %s value", got, envGeminiKey)
	}
	if strings.Contains(fs.requests[0].path, "projects/") {
		t.Errorf("the request took the Vertex AI path %q; the backend must stay the Gemini API", fs.requests[0].path)
	}
}

func TestTruncatedResponseIsAFailure(t *testing.T) {
	// Reasoning took the budget, so the advice is to lower reasoning effort.
	fs := newFakeServer(t, fakeCompletion{
		content: `{"questions":[`, finishReason: "MAX_TOKENS", thoughtTokens: 15900, answerTokens: 400,
	})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err == nil {
		t.Fatal("expected a truncated response to fail")
	}
	if !IsTruncated(err) {
		t.Errorf("error should be a TruncatedError, got %T: %v", err, err)
	}
	// The remedy is lowering reasoning effort, not shortening the RFP.
	if !strings.Contains(err.Error(), ConfReasoningStrategy) {
		t.Errorf("error should point at the reasoning config, got: %v", err)
	}
}

func TestRunawayAnswerIsNotBlamedOnReasoning(t *testing.T) {
	// The triage failure seen live: almost all of the budget went to the visible
	// answer — whitespace or repetition under a JSON schema — not to reasoning. Telling
	// the operator to lower reasoning effort would send them after the wrong thing.
	runaway := `{"sections":[{"name":"FR"` + strings.Repeat("\n", 2000)
	fs := newFakeServer(t, fakeCompletion{
		content: runaway, finishReason: "MAX_TOKENS", thoughtTokens: 3910, answerTokens: 12000,
	})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	var truncated *TruncatedError
	if !errors.As(err, &truncated) {
		t.Fatalf("error should be a TruncatedError, got %T: %v", err, err)
	}
	msg := err.Error()
	if strings.Contains(msg, ConfReasoningStrategy) {
		t.Errorf("an answer-dominated truncation must not prescribe a reasoning change: %v", msg)
	}
	for _, want := range []string{"3910 reasoning + 12000 answer tokens", "runaway", `{\"sections\"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should contain %q, got: %v", want, truncate(msg, 600))
		}
	}
	// Quoted, so a run of newlines is visible as escapes rather than as blank space.
	if !strings.Contains(msg, `\n\n\n`) {
		t.Errorf("the excerpt should show the whitespace run as escapes, got: %v", truncate(msg, 600))
	}
	if len(msg) > 4000 {
		t.Errorf("error message is %d bytes; the excerpt should be bounded", len(msg))
	}
}

func TestEmptyResponseIsAFailureNotAnAbsence(t *testing.T) {
	// This is the signature of reasoning consuming the whole output budget. Reading it
	// as "the model found nothing" would silently produce a manifest missing whatever
	// the call was for.
	fs := newFakeServer(t, fakeCompletion{thought: "thinking at length", finishReason: "MAX_TOKENS"})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err == nil {
		t.Fatal("expected an empty response to fail")
	}
	var truncated *TruncatedError
	if !errors.As(err, &truncated) {
		t.Fatalf("error should be a TruncatedError, got %T: %v", err, err)
	}
	if !truncated.Empty {
		t.Error("the error should record that the response was empty")
	}
}

func TestEmptyResponseWithStopReasonIsStillAFailure(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: "", finishReason: "STOP"})
	c := testClient(t, fs)

	if _, err := c.Triage(context.Background(), testConveyance(t), testCatalogue()); err == nil {
		t.Fatal("an empty response must fail regardless of the finish reason")
	}
}

func TestNoCandidatesIsAFailure(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{noChoices: true})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err == nil {
		t.Fatal("a response with no candidates must fail")
	}
	if IsBlocked(err) {
		t.Errorf("no candidates without a block reason is not a block: %v", err)
	}
}

func TestThoughtPartsAreNotParsedAsTheAnswer(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{thought: "Let me count {the rows", content: okTriage})
	c := testClient(t, fs)

	tr, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if tr.EstimatedQuestions != 5 {
		t.Errorf("estimated questions = %d, want 5 from the answer part", tr.EstimatedQuestions)
	}
}

func TestBlockedPromptIsReportedAsBlocked(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{noChoices: true, promptBlock: "PROHIBITED_CONTENT"})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error should be a BlockedError, got %T: %v", err, err)
	}
	if !blocked.Prompt || blocked.Call != "triage" || !strings.Contains(err.Error(), "PROHIBITED_CONTENT") {
		t.Errorf("the error should name the call and the reason, got: %v", err)
	}
}

func TestBlockedResponseIsNotATruncation(t *testing.T) {
	// A response cut short by a safety stop is still cut short, but calling it a
	// budget problem would send the operator off lowering reasoning effort for nothing.
	fs := newFakeServer(t, fakeCompletion{content: `{"questions":[`, finishReason: "SAFETY"})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if !IsBlocked(err) {
		t.Fatalf("error should be a BlockedError, got %T: %v", err, err)
	}
	if IsTruncated(err) || IsMalformedResponse(err) {
		t.Errorf("a blocked response must not be reported as truncated or malformed: %v", err)
	}
	if !strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("error should name the block reason, got: %v", err)
	}
}

func TestTriageRejectsAResponseMissingItsEstimate(t *testing.T) {
	// Well-formed JSON, parses cleanly, but carries no question-count estimate — which
	// would silently disable the later shortfall check. A schema does not prevent this.
	fs := newFakeServer(t, fakeCompletion{content: `{"title":"An RFP","openstack_in_scope":true,"multi_field_answers":false}`})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err == nil {
		t.Fatal("expected a triage response with no estimate to fail")
	}
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) {
		t.Fatalf("error should be an IncompleteError, got %T: %v", err, err)
	}
}

func TestTriageFiltersSuggestionsToTheCatalogue(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: `{
		"estimated_questions": 12,
		"openstack_in_scope": true,
		"multi_field_answers": false,
		"suggested_knowledge_bases": ["vendor-rfp", "invented-base"],
		"suggested_kapa_source_groups": ["Upstream docs", "made-up-id"]
	}`})
	c := testClient(t, fs)

	tr, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if len(tr.SuggestedKnowledgeBases) != 1 || tr.SuggestedKnowledgeBases[0] != "vendor-rfp" {
		t.Errorf("suggested knowledge bases = %v, want just the catalogued one", tr.SuggestedKnowledgeBases)
	}
	// A group named rather than identified is canonicalised to its identifier, since
	// that is what a run actually sends.
	if len(tr.SuggestedKapaSourceGroups) != 1 || tr.SuggestedKapaSourceGroups[0] != "aaaa-bbbb" {
		t.Errorf("suggested kapa groups = %v, want the catalogued identifier", tr.SuggestedKapaSourceGroups)
	}
	// The conveyance report and model are filled in by rag-cli, not the model.
	if len(tr.Report) != 1 {
		t.Errorf("triage should carry the conveyance report, got %d entries", len(tr.Report))
	}
	if tr.Model != "test-model" {
		t.Errorf("triage model = %q, want the configured model", tr.Model)
	}
}

func TestNotDocumentCapableModelIsReportedAsSuch(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{
		status:  http.StatusBadRequest,
		errBody: `{"error":{"code":400,"message":"Unsupported MIME type: application/pdf","status":"INVALID_ARGUMENT"}}`,
	})
	s := testSettings()
	s.Model = "gemini-3.8-flash-tts"
	c := testClientWith(t, fs, s)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if !IsNotDocumentCapable(err) {
		t.Fatalf("error should be a NotDocumentCapableError, got %T: %v", err, err)
	}
	for _, want := range []string{"gemini-3.8-flash-tts", ConfModel} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
}

func TestUnrelatedProviderErrorIsNotMisclassified(t *testing.T) {
	// A false positive would hide a real error, so an ordinary failure must pass
	// through as itself.
	fs := newFakeServer(t, fakeCompletion{
		status:  http.StatusInternalServerError,
		errBody: `{"error":{"code":500,"message":"An internal error has occurred.","status":"INTERNAL"}}`,
	})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err == nil {
		t.Fatal("expected the request to fail")
	}
	if IsNotDocumentCapable(err) || IsAuth(err) || IsModelNotFound(err) {
		t.Errorf("an internal error must pass through unclassified, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "triage") || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("error should name the call and carry the provider's message, got: %v", err)
	}
}

func TestPDFIsSentInlineAsADocumentPart(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: `{"estimated_questions":3,"openstack_in_scope":false,"multi_field_answers":false}`})
	c := testClient(t, fs)

	const pdf = "%PDF-1.7\nbody\n"
	conv, err := NewConverter("").ConveyAll(writeFile(t, "rfp.pdf", pdf), nil)
	if err != nil {
		t.Fatalf("ConveyAll: %v", err)
	}
	if _, err := c.Triage(context.Background(), conv, testCatalogue()); err != nil {
		t.Fatalf("Triage: %v", err)
	}

	body := fs.requests[0].rawBody()
	if !strings.Contains(body, `"mimeType":"application/pdf"`) {
		t.Errorf("a PDF must be sent as an application/pdf inline part, got: %s", truncate(body, 400))
	}
	if !strings.Contains(body, base64.StdEncoding.EncodeToString([]byte(pdf))) {
		t.Errorf("the inline part must carry the original bytes, got: %s", truncate(body, 400))
	}
	// The label carries the filename, since an inline part has no name of its own.
	if !strings.Contains(body, "rfp.pdf") {
		t.Errorf("the request must name the document, got: %s", truncate(body, 400))
	}
	// One request, no upload: the Files API would retain the document server-side.
	if len(fs.requests) != 1 || strings.Contains(fs.requests[0].path, "files") {
		t.Errorf("the PDF must travel inside the generation request, got requests %v", fs.requests)
	}
}

func TestConvertedDocumentIsSentAsText(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: `{"estimated_questions":3,"openstack_in_scope":false,"multi_field_answers":false}`})
	c := testClient(t, fs)

	if _, err := c.Triage(context.Background(), testConveyance(t), testCatalogue()); err != nil {
		t.Fatalf("Triage: %v", err)
	}
	body := fs.requests[0].rawBody()
	if strings.Contains(body, "inlineData") {
		t.Error("a converted document must not be sent as an inline data part")
	}
	// The Markdown table, with its context columns, must be in the request.
	if !strings.Contains(body, "Evaluator Guidance") {
		t.Errorf("the converted table's columns are missing from the request: %s", truncate(body, 400))
	}
}

func TestKeyTravelsInTheHeaderNeverInTheBody(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: `{"estimated_questions":3,"openstack_in_scope":false,"multi_field_answers":false}`})
	c := testClient(t, fs)

	if _, err := c.Triage(context.Background(), testConveyance(t), testCatalogue()); err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if strings.Contains(fs.requests[0].rawBody(), testKey) {
		t.Error("the API key must never appear in the request body")
	}
	if fs.requests[0].apiKey != testKey {
		t.Errorf("x-goog-api-key = %q, want the configured key", fs.requests[0].apiKey)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("… (%d bytes total)", len(s))
}

func TestInvalidKeyReportedAs400IsAnAuthFailure(t *testing.T) {
	// Gemini reports a bad key as 400 INVALID_ARGUMENT, not 401. A status-only check
	// would file the commonest auth failure as an ordinary bad request.
	fs := newFakeServer(t, fakeCompletion{
		status: http.StatusBadRequest,
		errBody: `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.",` +
			`"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
			`"reason":"API_KEY_INVALID","domain":"googleapis.com"}]}}`,
	})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if !IsAuth(err) {
		t.Fatalf("error should be an AuthError, got %T: %v", err, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, envGeminiKey) {
		t.Errorf("error should name %s, got: %v", envGeminiKey, msg)
	}
	if !strings.Contains(msg, "API key not valid") {
		t.Errorf("error should carry the provider's message, got: %v", msg)
	}
	if strings.Contains(msg, testKey) {
		t.Errorf("the API key value leaked into the error: %v", msg)
	}
}

func TestForbiddenIsAnAuthFailureWithTheAPIHint(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{
		status:  http.StatusForbidden,
		errBody: `{"error":{"code":403,"message":"Method doesn't allow unregistered callers.","status":"PERMISSION_DENIED"}}`,
	})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if !IsAuth(err) {
		t.Fatalf("error should be an AuthError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "Generative Language API") {
		t.Errorf("a 403 should mention the API may not be enabled, got: %v", err)
	}
}

func TestRateLimitIsAQuotaErrorNotAuth(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{
		status:  http.StatusTooManyRequests,
		errBody: `{"error":{"code":429,"message":"Resource has been exhausted.","status":"RESOURCE_EXHAUSTED"}}`,
	})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if IsAuth(err) {
		t.Errorf("a rate-limit error must not be classified as auth: %v", err)
	}
	var quota *QuotaError
	if !errors.As(err, &quota) {
		t.Fatalf("error should be a QuotaError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "billing-enabled") {
		t.Errorf("a quota error should point at the project's billing state, got: %v", err)
	}
}

func TestUnknownModelNamesTheKey(t *testing.T) {
	const notFound = `{"error":{"code":404,"message":"models/%s is not found for API version v1beta","status":"NOT_FOUND"}}`

	t.Run("gateway prefix", func(t *testing.T) {
		// The predictable failure on any install configured for OpenRouter.
		s := testSettings()
		s.Model = "google/gemini-3.1-pro-preview"
		fs := newFakeServer(t, fakeCompletion{status: http.StatusNotFound, errBody: fmt.Sprintf(notFound, s.Model)})
		c := testClientWith(t, fs, s)

		_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
		if !IsModelNotFound(err) {
			t.Fatalf("error should be a ModelNotFoundError, got %T: %v", err, err)
		}
		for _, want := range []string{s.Model, ConfModel, "unprefixed"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should contain %q, got: %v", want, err)
			}
		}
	})

	t.Run("plain id", func(t *testing.T) {
		s := testSettings()
		s.Model = "gemini-9-ultra"
		fs := newFakeServer(t, fakeCompletion{status: http.StatusNotFound, errBody: fmt.Sprintf(notFound, s.Model)})
		c := testClientWith(t, fs, s)

		_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
		if !IsModelNotFound(err) {
			t.Fatalf("error should be a ModelNotFoundError, got %T: %v", err, err)
		}
		if strings.Contains(err.Error(), "unprefixed") {
			t.Errorf("an unprefixed id must not get the gateway hint: %v", err)
		}
	})
}

func TestMalformedResponseReportsTheEvidence(t *testing.T) {
	// Cut-off JSON with a finish reason that says the model completed normally: the
	// parser alone cannot distinguish this from a model that ignored the schema, so
	// the error has to carry the finish reason and the payload.
	const cut = `{"questions":[{"id":"C1","question":"SR-IOV","source":"C"},{"id":"C2","`
	fs := newFakeServer(t, fakeCompletion{content: cut, finishReason: "STOP"})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
	if !IsMalformedResponse(err) {
		t.Fatalf("error should be a MalformedResponseError, got %T: %v", err, err)
	}
	msg := err.Error()
	for _, want := range []string{`"STOP"`, "bytes", "SR-IOV"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should carry %q as evidence, got: %v", want, msg)
		}
	}
	// A clean finish means the model returned non-JSON, so the advice must point at
	// the model, not at the reasoning budget.
	if !strings.Contains(msg, ConfModel) {
		t.Errorf("a clean finish should point at %s, got: %v", ConfModel, msg)
	}
}

func TestMalformedResponseWithNoFinishReasonSuggestsARetry(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: `{"questions":[`, omitFinish: true})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if !IsMalformedResponse(err) {
		t.Fatalf("expected a MalformedResponseError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("a missing finish reason should suggest an interrupted stream, got: %v", err)
	}
}

func TestMalformedResponseExcerptIsBounded(t *testing.T) {
	// A 200-question response should not paste itself into the terminal in full.
	huge := `{"questions":[` + strings.Repeat(`{"id":"X","question":"padding padding"},`, 500)
	fs := newFakeServer(t, fakeCompletion{content: huge, finishReason: "STOP"})
	c := testClient(t, fs)

	_, err := c.Triage(context.Background(), testConveyance(t), testCatalogue())
	if !IsMalformedResponse(err) {
		t.Fatalf("expected a MalformedResponseError, got %T: %v", err, err)
	}
	msg := err.Error()
	if len(msg) > 4000 {
		t.Errorf("error message is %d bytes; the excerpt should be bounded", len(msg))
	}
	// Both ends are shown, because where it stops is the diagnostic.
	if !strings.Contains(msg, "response began:") || !strings.Contains(msg, "and ended:") {
		t.Errorf("a long payload should show head and tail, got: %v", msg[:200])
	}
}
