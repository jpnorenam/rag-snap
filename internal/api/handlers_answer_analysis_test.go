package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/analysis"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

func TestAnalyzeRejectedWhenNotConfigured(t *testing.T) {
	// Analysis off by default: the endpoint must refuse rather than attempt a call, and
	// must say what is missing.
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("ANALYSIS_API_KEY", "")
	sock, _ := startTestServer(t, map[string]string{backendTika: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	body, contentType := multipartFile(t, "rfp.csv", "Req,Text\nC1,SR-IOV\n", nil)
	resp, err := client.Post("http://unix/1.0/answer/analyze", contentType, body)
	if err != nil {
		t.Fatalf("POST /1.0/answer/analyze: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	if !strings.Contains(text, "analysis") {
		t.Errorf("error should explain that analysis is unavailable, got: %s", text)
	}
	// Naming the variables to set is what makes the refusal actionable.
	if !strings.Contains(text, "GEMINI_API_KEY") && !strings.Contains(text, "analysis.enabled") {
		t.Errorf("error should name what is missing, got: %s", text)
	}
}

func TestAnalyzeCapabilityAdvertisedOnlyWhenAvailable(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("ANALYSIS_API_KEY", "")
	sock, _ := startTestServer(t, map[string]string{})
	client := dialSocket(sock)

	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	var env struct {
		Metadata struct {
			Extensions []string `json:"api_extensions"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	var hasAnalyze, hasCheck bool
	for _, e := range env.Metadata.Extensions {
		switch e {
		case "answer_analyze":
			hasAnalyze = true
		case "answer_check":
			hasCheck = true
		}
	}
	// A client uses this to present the path as unavailable rather than offering it and
	// failing at submit time, so advertising it while unconfigured defeats the purpose.
	if hasAnalyze {
		t.Error("answer_analyze must not be advertised while analysis is unconfigured")
	}
	// The check needs nothing beyond the manifest, so it is always available.
	if !hasCheck {
		t.Error("answer_check should always be advertised")
	}
}

func TestAnalyzeGenerateRejectsUnknownToken(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{})
	client := dialSocket(sock)

	resp, err := client.Post("http://unix/1.0/answer/analyze/generate", "application/json",
		strings.NewReader(`{"analysis_token":"deadbeef","answer_words":100,"answer_limit":"soft"}`))
	if err != nil {
		t.Fatalf("POST generate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	// Recoverable, and it says how to recover: the UI returns to upload rather than
	// dead-ending.
	if !strings.Contains(string(raw), "expired") {
		t.Errorf("error should say the session expired, got: %s", raw)
	}
	if !strings.Contains(string(raw), "upload the documents again") {
		t.Errorf("error should say how to recover, got: %s", raw)
	}
}

func TestAnalyzeGenerateRejectsAnInvalidInterview(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{})
	client := dialSocket(sock)

	// A bad length mode is rejected before the token is even considered valid.
	resp, err := client.Post("http://unix/1.0/answer/analyze/generate", "application/json",
		strings.NewReader(`{"analysis_token":"x","answer_words":100,"answer_limit":"approximate"}`))
	if err != nil {
		t.Fatalf("POST generate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAnswerCheckReturnsFindingsSynchronously(t *testing.T) {
	// No knowledge store reachable, so the reference checks must report as not
	// performed rather than as passing — and the rest of the check must still run.
	sock, _ := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	body := `{
	  "version": "1.0",
	  "knowledge_bases": ["vendor-rfp"],
	  "prompt": "Be direct, no filler. At most 100 words. Say when no context is available.",
	  "domains": [
	    {"match": "C*", "context": "Section C."},
	    {"match": "GIS*", "context": "Reserved."}
	  ],
	  "questions": [
	    {"id": "C1", "question": "SR-IOV", "source": "C"},
	    {"id": "C1", "question": "CPU pinning", "source": "C"}
	  ]
	}`
	resp, err := client.Post("http://unix/1.0/answer/check", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/answer/check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, raw)
	}

	var env struct {
		Metadata chat.CheckResult `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	got := env.Metadata

	if got.Questions != 2 || got.Domains != 2 {
		t.Errorf("counts = %d/%d, want 2 questions and 2 domains", got.Questions, got.Domains)
	}
	if len(got.DomainMatches) != 2 {
		t.Errorf("expected per-domain match counts, got %+v", got.DomainMatches)
	}
	// The duplicate id is a defect; the reserved pattern is only a warning.
	if !got.HasDefects() {
		t.Error("the duplicated question id should be reported as a defect")
	}
	if len(got.Warnings()) == 0 {
		t.Error("the zero-match pattern should be reported as a warning")
	}
	if got.ReferencesChecked {
		t.Error("references must be reported as not checked when the knowledge store is unreachable")
	}
	if got.KapaChecked {
		t.Error("kapa must be reported as not checked: the daemon does not wire kapa retrieval")
	}
}

func TestAnswerCheckRejectsAManifestWithNoQuestions(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{})
	client := dialSocket(sock)

	resp, err := client.Post("http://unix/1.0/answer/check", "application/json",
		strings.NewReader(`{"version":"1.0","questions":[]}`))
	if err != nil {
		t.Fatalf("POST check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAnswerCheckDoesNotFlagKapaGroupsItCannotVerify(t *testing.T) {
	// The daemon has no kapa client, so a manifest naming kapa groups must not have
	// them reported as non-existent — that would be a fabricated finding on a valid
	// manifest.
	sock, _ := startTestServer(t, map[string]string{})
	client := dialSocket(sock)

	body := `{
	  "version": "1.0",
	  "prompt": "Be direct, no filler. At most 100 words. Say when no context is available.",
	  "questions": [{"id": "C1", "question": "SR-IOV"}]
	}`
	resp, err := client.Post("http://unix/1.0/answer/check", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST check: %v", err)
	}
	defer resp.Body.Close()

	var env struct {
		Metadata chat.CheckResult `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	for _, f := range env.Metadata.Findings {
		if f.Code == chat.CheckUnknownKapaGroup {
			t.Errorf("kapa group reported as unknown although kapa was never consulted: %s", f.Message)
		}
	}
}

// TestGeneratedManifestShapeIsAcceptedByTheBatchEndpoint checks the contract that makes
// the generation metadata directly runnable: the manifest published on the operation is
// in the shape POST /1.0/answer/batch accepts, with no field dropped in transit.
func TestGeneratedManifestShapeIsAcceptedByTheBatchEndpoint(t *testing.T) {
	generated := &chat.BatchManifest{
		Version:        "1.0",
		KnowledgeBases: []string{"vendor-rfp"},
		Prompt:         "Be direct, no filler. At most 100 words. Say when no context is available.",
		Domains: []chat.Domain{
			{Match: "C*", Context: "Section C.", Keywords: chat.KeywordList{"SR-IOV"}},
		},
		Questions: []chat.BatchQuestion{
			{ID: "C1", Question: "SR-IOV", Source: "C", Keywords: chat.KeywordList{"NUMA"}},
		},
	}

	// The exact conversion the generation handler publishes.
	shaped := manifestToJSON(generated)
	raw, err := json.Marshal(shaped)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}

	// Round-trip it back through the batch endpoint's own request type, which is what
	// a client would post unchanged.
	var req batchManifestRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("the published shape does not decode as a batch request: %v", err)
	}
	round := req.toManifest()

	if round.Version != generated.Version {
		t.Errorf("version dropped: %q", round.Version)
	}
	if len(round.KnowledgeBases) != 1 || round.KnowledgeBases[0] != "vendor-rfp" {
		t.Errorf("knowledge_bases dropped: %v", round.KnowledgeBases)
	}
	if round.Prompt != generated.Prompt {
		t.Error("prompt dropped or altered in transit")
	}
	if len(round.Domains) != 1 || round.Domains[0].Match != "C*" || round.Domains[0].Context != "Section C." {
		t.Errorf("domains dropped: %+v", round.Domains)
	}
	if len(round.Domains[0].Keywords) != 1 {
		t.Errorf("domain keywords dropped: %+v", round.Domains[0].Keywords)
	}
	if len(round.Questions) != 1 || round.Questions[0].Source != "C" {
		t.Errorf("question source dropped: %+v", round.Questions)
	}
	if len(round.Questions[0].Keywords) != 1 {
		t.Errorf("question keywords dropped: %+v", round.Questions[0].Keywords)
	}
	// The manifest must not pin the answering model.
	if round.Model != "" {
		t.Error("a generated manifest must not pin the answering model")
	}
	// And it must still compile and route.
	set, err := chat.CompileDomains(round.Domains)
	if err != nil {
		t.Fatalf("round-tripped domains do not compile: %v", err)
	}
	if d := set.Resolve("C1", "C"); d == nil {
		t.Error("round-tripped routing no longer resolves")
	}
}

func TestAnalysisStoreBoundsAndExpiry(t *testing.T) {
	store := newAnalysisStore()

	conv := &analysis.Conveyance{Documents: []analysis.Document{{Text: strings.Repeat("x", 1024)}}}
	token, err := store.stage(conv, &analysis.Triage{EstimatedQuestions: 1}, &analysis.Catalogue{})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, ok := store.get(token); !ok {
		t.Fatal("a freshly staged entry should be retrievable")
	}
	if _, ok := store.expiresAt(token); !ok {
		t.Error("a staged entry should report an expiry, so a client can show the session's lifetime")
	}

	// Consuming releases the memory as well as the token: generation is the end of the
	// session, so documents are not left resident for the rest of the TTL.
	store.consume(token)
	if _, ok := store.get(token); ok {
		t.Error("a consumed token should no longer resolve")
	}
	if store.total != 0 {
		t.Errorf("held bytes = %d after consume, want 0", store.total)
	}
}

func TestAnalysisStoreRejectsAnOversizedRequest(t *testing.T) {
	if err := checkAnalysisRequestSize(maxAnalysisRequestSize + 1); err == nil {
		t.Fatal("an over-large request should be rejected")
	} else if !strings.Contains(err.Error(), "limit") {
		t.Errorf("rejection should name the limit, got: %v", err)
	}
	if err := checkAnalysisRequestSize(1024); err != nil {
		t.Errorf("a small request should be accepted: %v", err)
	}
}

func TestAnalysisStoreEvictsOldestWhenFull(t *testing.T) {
	store := newAnalysisStore()
	conv := func() *analysis.Conveyance {
		return &analysis.Conveyance{Documents: []analysis.Document{{Text: strings.Repeat("x", 512)}}}
	}

	var first string
	for i := 0; i < maxStagedAnalyses+1; i++ {
		token, err := store.stage(conv(), &analysis.Triage{}, &analysis.Catalogue{})
		if err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
		if i == 0 {
			first = token
		}
	}
	if _, ok := store.get(first); ok {
		t.Error("the oldest entry should have been evicted once the cap was exceeded")
	}
	if len(store.entries) > maxStagedAnalyses {
		t.Errorf("held %d entries, above the cap of %d", len(store.entries), maxStagedAnalyses)
	}
}

func TestCheckEndpointCarriesSingleQuestionPreviewAndDomainTextRule(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	body := `{
	  "version": "1.0",
	  "knowledge_bases": ["vendor-rfp"],
	  "prompt": "Be direct, no filler. At most 100 words. Say when no context is available. Requirement domain text is guidance, not a source.",
	  "domains": [{"match": "T4-iii", "context": "CIS image management."}],
	  "questions": [{"id": "T4-iii", "question": "Can it run without foreign dependency?"}]
	}`
	resp, err := client.Post("http://unix/1.0/answer/check", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/answer/check: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, raw)
	}
	// Asserted on the wire, not only through the Go type, because the UI reads these
	// names directly.
	for _, want := range []string{
		`"only_question":{"id":"T4-iii","text":"Can it run without foreign dependency?"}`,
		`"context_preview":"CIS image management."`,
		`"domain_text_rule":true`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("check response is missing %s: %s", want, raw)
		}
	}
}

// stageForTest puts a minimal analysis session in the server's store and returns its token.
func stageForTest(t *testing.T, srv *Server) string {
	t.Helper()
	token, err := srv.analyses.stage(&analysis.Conveyance{}, &analysis.Triage{EstimatedQuestions: 10}, &analysis.Catalogue{})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	return token
}

func postGenerate(t *testing.T, client *http.Client, body string) (int, string) {
	t.Helper()
	resp, err := client.Post("http://unix/1.0/answer/analyze/generate", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST generate: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestGenerateDecisionRequiresARetainedShortfall(t *testing.T) {
	sock, srv := startTestServer(t, map[string]string{})
	client := dialSocket(sock)
	token := stageForTest(t, srv)

	for _, flag := range []string{"accept_shortfall", "retry_questions"} {
		status, body := postGenerate(t, client, fmt.Sprintf(
			`{"analysis_token":%q,"answer_words":100,"answer_limit":"soft",%q:true}`, token, flag))
		if status != http.StatusBadRequest || !strings.Contains(body, "no shortfall") {
			t.Errorf("%s without a shortfall: status %d, body %s — want a 400 saying there is no shortfall", flag, status, body)
		}
	}

	status, body := postGenerate(t, client, fmt.Sprintf(
		`{"analysis_token":%q,"answer_words":100,"answer_limit":"soft","accept_shortfall":true,"retry_questions":true}`, token))
	if status != http.StatusBadRequest || !strings.Contains(body, "alternatives") {
		t.Errorf("both decisions at once: status %d, body %s — want a 400", status, body)
	}
}

func TestRetryIsRefusedAtTheTopOfTheEffortLadder(t *testing.T) {
	sock, srv := startTestServer(t, map[string]string{})
	client := dialSocket(sock)
	token := stageForTest(t, srv)
	srv.analyses.retainShortfall(token, &analysis.ShortfallError{
		Expected: 10, Produced: 3, Effort: analysis.EffortHigh,
		Questions: []chat.BatchQuestion{{ID: "C1", Question: "SR-IOV"}},
	})

	status, body := postGenerate(t, client, fmt.Sprintf(
		`{"analysis_token":%q,"answer_words":100,"answer_limit":"soft","retry_questions":true}`, token))
	if status != http.StatusBadRequest || !strings.Contains(body, "highest reasoning effort") {
		t.Errorf("retry at high: status %d, body %s — want a 400 naming the top of the ladder", status, body)
	}
}

func TestRetainedShortfallCountsAgainstTheStoreBound(t *testing.T) {
	store := newAnalysisStore()
	token, err := store.stage(&analysis.Conveyance{}, &analysis.Triage{}, &analysis.Catalogue{})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	before := store.total

	qs := []chat.BatchQuestion{{ID: "C1", Question: "SR-IOV", Source: "C"}, {ID: "C2", Question: "NUMA", Source: "C"}}
	if !store.retainShortfall(token, &analysis.ShortfallError{Expected: 9, Produced: 2, Effort: analysis.EffortLow, Questions: qs}) {
		t.Fatal("retainShortfall should succeed on a live session")
	}
	if got, want := store.total-before, questionsSize(qs); got != want {
		t.Errorf("store grew by %d bytes, want %d for the retained questions", got, want)
	}

	// A second shortfall replaces the first rather than adding to it.
	qs2 := qs[:1]
	store.retainShortfall(token, &analysis.ShortfallError{Expected: 9, Produced: 1, Questions: qs2})
	if got, want := store.total-before, questionsSize(qs2); got != want {
		t.Errorf("after replacing, store holds %d extra bytes, want %d", got, want)
	}
	sf, ok := store.shortfallFor(token)
	if !ok || sf.produced != 1 || len(sf.questions) != 1 {
		t.Errorf("shortfallFor = %+v, %v; want the replacing shortfall", sf, ok)
	}

	if store.retainShortfall("no-such-token", &analysis.ShortfallError{}) {
		t.Error("retainShortfall must report false for an unknown session")
	}
}

func TestShortfallMetadataCarriesTheDecisionEvidence(t *testing.T) {
	meta := shortfallMetadata(&analysis.ShortfallError{
		Expected: 220, Produced: 53, FinishReason: "STOP", AnswerTokens: 9000, Ceiling: 65536,
		Effort:   analysis.EffortLow,
		Sections: []analysis.SectionCount{{Name: "NFR", IDPrefix: "NFR_", Estimated: 115, Produced: 22}},
	})
	raw, _ := json.Marshal(meta)
	for _, want := range []string{
		`"expected":220`, `"produced":53`, `"finish_reason":"STOP"`, `"effort":"low"`, `"next_effort":"medium"`,
		`"sections":[{"name":"NFR","id_prefix":"NFR_","estimated":115,"produced":22}]`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("shortfall metadata is missing %s: %s", want, raw)
		}
	}
	// The produced questions stay on the server; they are not shipped to the client.
	if strings.Contains(string(raw), "questions") {
		t.Errorf("shortfall metadata must not carry the question list: %s", raw)
	}
}
