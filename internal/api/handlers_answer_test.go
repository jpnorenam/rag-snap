package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAnswerBatchRunsAndStoresResults posts a prepared manifest, waits for the
// async operation to complete, and verifies the structured results are
// retrievable with each question, its answer, the resolved model, and a
// generation timestamp. The OpenSearch backend is unreachable, so retrieval
// yields nothing and every answer is the fixed no-context response — also
// covering the rest-api-answer "no retrieved context" scenario.
func TestAnswerBatchRunsAndStoresResults(t *testing.T) {
	inference := stubInference(t)
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	body := `{
		"version": "1.0",
		"questions": [
			{"id": "q1", "question": "What is the capital?"},
			{"id": "q2", "question": "How tall is the tower?"}
		]
	}`
	resp, err := client.Post("http://unix/1.0/answer/batch", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/answer/batch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 202; body=%s", resp.StatusCode, b)
	}

	var env struct {
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding async envelope: %v", err)
	}
	if env.Operation == "" {
		t.Fatal("async envelope missing operation URL")
	}

	// Wait for the operation to reach a terminal state, then read the results
	// from its metadata.
	wresp, err := client.Get("http://unix" + env.Operation + "/wait?timeout=10")
	if err != nil {
		t.Fatalf("GET operation wait: %v", err)
	}
	defer wresp.Body.Close()

	var waitEnv struct {
		Metadata struct {
			StatusCode int `json:"status_code"`
			Err        string
			Metadata   struct {
				Model       string `json:"model"`
				GeneratedAt string `json:"generated_at"`
				Results     []struct {
					ID       string `json:"id"`
					Question string `json:"question"`
					Answer   string `json:"answer"`
				} `json:"results"`
			} `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(wresp.Body).Decode(&waitEnv); err != nil {
		t.Fatalf("decoding wait response: %v", err)
	}

	meta := waitEnv.Metadata
	if meta.StatusCode != statusCodeSuccess {
		t.Fatalf("operation status = %d (%s), want success", meta.StatusCode, meta.Err)
	}
	if meta.Metadata.Model != "stub-model" {
		t.Errorf("model = %q, want stub-model", meta.Metadata.Model)
	}
	if meta.Metadata.GeneratedAt == "" {
		t.Error("generated_at is empty")
	} else if _, err := time.Parse(time.RFC3339, meta.Metadata.GeneratedAt); err != nil {
		t.Errorf("generated_at = %q is not RFC3339: %v", meta.Metadata.GeneratedAt, err)
	}
	if len(meta.Metadata.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(meta.Metadata.Results))
	}
	if meta.Metadata.Results[0].ID != "q1" || meta.Metadata.Results[0].Question != "What is the capital?" {
		t.Errorf("result[0] = %+v, want id q1 with its question", meta.Metadata.Results[0])
	}
	for i, r := range meta.Metadata.Results {
		if !strings.Contains(r.Answer, "does not contain enough information") {
			t.Errorf("result[%d] answer = %q, want the fixed no-context response", i, r.Answer)
		}
	}
}

// TestAnswerBatchRejectsEmptyManifest verifies a manifest with no questions is
// rejected synchronously with 400.
func TestAnswerBatchRejectsEmptyManifest(t *testing.T) {
	inference := stubInference(t)
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	resp, err := client.Post("http://unix/1.0/answer/batch", "application/json", strings.NewReader(`{"questions": []}`))
	if err != nil {
		t.Fatalf("POST /1.0/answer/batch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// buildInspectResult is the pass-1 metadata for a tabular build.
type buildInspectResult struct {
	NeedsColumn bool   `json:"needs_column"`
	BuildToken  string `json:"build_token"`
	Format      string `json:"format"`
	Tables      []struct {
		Name    string   `json:"name"`
		Header  []string `json:"header"`
		Columns []struct {
			Index     int      `json:"index"`
			Sample    []string `json:"sample"`
			AvgLen    int      `json:"avg_len"`
			Suggested bool     `json:"suggested"`
		} `json:"columns"`
	} `json:"tables"`
	Suggested struct {
		TableIndex  int `json:"table_index"`
		ColumnIndex int `json:"column_index"`
	} `json:"suggested"`
}

// buildExtractResult is the pass-2 (and free-text) metadata.
type buildExtractResult struct {
	Questions []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
	} `json:"questions"`
}

// waitOpMeta waits for an operation and decodes its terminal metadata into dst,
// asserting success.
func waitOpMeta(t *testing.T, client *http.Client, opURL string, dst any) {
	t.Helper()
	wresp, err := client.Get("http://unix" + opURL + "/wait?timeout=10")
	if err != nil {
		t.Fatalf("GET operation wait: %v", err)
	}
	defer wresp.Body.Close()
	var env struct {
		Metadata struct {
			StatusCode int             `json:"status_code"`
			Err        string          `json:"err"`
			Metadata   json.RawMessage `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(wresp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding wait response: %v", err)
	}
	if env.Metadata.StatusCode != statusCodeSuccess {
		t.Fatalf("operation status = %d (%s), want success", env.Metadata.StatusCode, env.Metadata.Err)
	}
	if err := json.Unmarshal(env.Metadata.Metadata, dst); err != nil {
		t.Fatalf("decoding op metadata: %v", err)
	}
}

// postBuild uploads a document to POST /1.0/answer/build and returns the async
// operation URL.
func postBuild(t *testing.T, client *http.Client, filename, content string, fields map[string]string) string {
	t.Helper()
	body, contentType := multipartFile(t, filename, content, fields)
	resp, err := client.Post("http://unix/1.0/answer/build", contentType, body)
	if err != nil {
		t.Fatalf("POST /1.0/answer/build: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 202; body=%s", resp.StatusCode, b)
	}
	var env struct {
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding async envelope: %v", err)
	}
	return env.Operation
}

// TestAnswerBuildCSVInspectThenExtract verifies the two-pass tabular flow: pass 1
// (POST /1.0/answer/build) returns the parsed table plus a build token and a
// suggested question column without extracting; pass 2 (build/extract) extracts
// from the chosen column. Refinement disabled so no inference backend is needed.
func TestAnswerBuildCSVInspectThenExtract(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	// Column 0 is an ID column (short/numeric); column 1 holds the questions.
	csv := "id,requirement\n" +
		"1,What is your data retention policy in detail?\n" +
		"2,Do you support single sign-on via SAML or OIDC?\n"
	opURL := postBuild(t, client, "rfp.csv", csv, map[string]string{"refine": "false"})

	var inspect buildInspectResult
	waitOpMeta(t, client, opURL, &inspect)
	if !inspect.NeedsColumn {
		t.Fatal("pass 1 should require a column choice for CSV")
	}
	if inspect.BuildToken == "" {
		t.Fatal("pass 1 should return a build token")
	}
	// The heuristic should prefer the prose column (1), not the ID column (0).
	if inspect.Suggested.ColumnIndex != 1 {
		t.Errorf("suggested column = %d, want 1 (the requirement column)", inspect.Suggested.ColumnIndex)
	}

	// Pass 2: extract the chosen column.
	extractBody := fmt.Sprintf(`{"build_token":%q,"table_index":0,"column_index":1,"refine":false}`, inspect.BuildToken)
	resp, err := client.Post("http://unix/1.0/answer/build/extract", "application/json", strings.NewReader(extractBody))
	if err != nil {
		t.Fatalf("POST /1.0/answer/build/extract: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("extract status = %d, want 202; body=%s", resp.StatusCode, b)
	}
	var env struct {
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding extract envelope: %v", err)
	}

	var extract buildExtractResult
	waitOpMeta(t, client, env.Operation, &extract)
	if len(extract.Questions) != 2 {
		t.Fatalf("got %d extracted questions, want 2", len(extract.Questions))
	}
	if !strings.Contains(extract.Questions[0].Question, "data retention") {
		t.Errorf("question[0] = %q, want the first requirement row", extract.Questions[0].Question)
	}
}

// TestAnswerBuildInspectSamplesNeverNull is the regression for the xlsx page
// crash: a column with no non-empty data cells must serialize its "sample" as a
// JSON array ([]), never null, so the client can index it without crashing. A
// real Tika-parsed sheet routinely has such (blank/spacer) columns.
func TestAnswerBuildInspectSamplesNeverNull(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	// Column 2 ("notes") has a header but no data values — its sample is empty.
	csv := "id,requirement,notes\n" +
		"1,What is your data retention policy in detail?,\n" +
		"2,Do you support single sign-on via SAML or OIDC?,\n"
	opURL := postBuild(t, client, "rfp.csv", csv, map[string]string{"refine": "false"})

	// Read the raw operation metadata so we can assert on the JSON, not a
	// decoded (and thus null-tolerant) Go struct.
	wresp, err := client.Get("http://unix" + opURL + "/wait?timeout=10")
	if err != nil {
		t.Fatalf("GET operation wait: %v", err)
	}
	defer wresp.Body.Close()
	raw, _ := io.ReadAll(wresp.Body)
	if strings.Contains(string(raw), `"sample":null`) {
		t.Fatalf("a column sample serialized as null (crashes the UI); body=%s", raw)
	}

	// And confirm the empty column decodes to a non-nil empty slice.
	var env struct {
		Metadata struct {
			Metadata buildInspectResult `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	var notesCol *struct {
		Index     int      `json:"index"`
		Sample    []string `json:"sample"`
		AvgLen    int      `json:"avg_len"`
		Suggested bool     `json:"suggested"`
	}
	for i := range env.Metadata.Metadata.Tables[0].Columns {
		if env.Metadata.Metadata.Tables[0].Columns[i].Index == 2 {
			notesCol = &env.Metadata.Metadata.Tables[0].Columns[i]
		}
	}
	if notesCol == nil {
		t.Fatal("notes column (index 2) missing from inspect result")
	}
	if notesCol.Sample == nil {
		t.Error("empty column sample decoded as nil; want non-nil empty slice")
	}
}

// TestAnswerBuildExtractUnknownToken verifies an unknown/expired build token is
// rejected with 400.
func TestAnswerBuildExtractUnknownToken(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	resp, err := client.Post("http://unix/1.0/answer/build/extract", "application/json",
		strings.NewReader(`{"build_token":"deadbeef","table_index":0,"column_index":0}`))
	if err != nil {
		t.Fatalf("POST /1.0/answer/build/extract: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestAnswerBuildExtractEmptyColumnFails verifies that choosing a column that
// yields no questions after the min-length filter fails the operation (so the
// client can retry a different column) rather than succeeding empty.
func TestAnswerBuildExtractEmptyColumnFails(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	csv := "id,requirement\n1,Describe your incident response process in full.\n"
	opURL := postBuild(t, client, "rfp.csv", csv, map[string]string{"refine": "false"})
	var inspect buildInspectResult
	waitOpMeta(t, client, opURL, &inspect)

	// Extract the ID column (0) with the default min-length (20): its cells are
	// short, so nothing passes the filter → the operation must fail.
	extractBody := fmt.Sprintf(`{"build_token":%q,"table_index":0,"column_index":0,"refine":false}`, inspect.BuildToken)
	resp, err := client.Post("http://unix/1.0/answer/build/extract", "application/json", strings.NewReader(extractBody))
	if err != nil {
		t.Fatalf("POST /1.0/answer/build/extract: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("extract status = %d, want 202 (async, fails later)", resp.StatusCode)
	}
	var env struct {
		Operation string `json:"operation"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&env)

	wresp, err := client.Get("http://unix" + env.Operation + "/wait?timeout=10")
	if err != nil {
		t.Fatalf("GET operation wait: %v", err)
	}
	defer wresp.Body.Close()
	var waitEnv struct {
		Metadata struct {
			StatusCode int `json:"status_code"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(wresp.Body).Decode(&waitEnv); err != nil {
		t.Fatalf("decoding wait response: %v", err)
	}
	if waitEnv.Metadata.StatusCode == statusCodeSuccess {
		t.Fatal("extracting an all-short column should fail, not succeed empty")
	}
}

// TestAnswerBuildRejectsUnsupportedType verifies an unsupported file type is
// rejected synchronously with 400.
func TestAnswerBuildRejectsUnsupportedType(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	body, contentType := multipartFile(t, "notes.txt", "not a supported document", nil)
	resp, err := client.Post("http://unix/1.0/answer/build", contentType, body)
	if err != nil {
		t.Fatalf("POST /1.0/answer/build: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// multipartFile builds a multipart/form-data body with a single file field
// plus optional extra form fields, returning the body and its Content-Type.
func multipartFile(t *testing.T, filename, content string, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(fw, content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// TestAnswerBatchPromptRefValidation verifies the request-level guards on
// prompt_ref: it is mutually exclusive with an inline prompt, and an unknown
// variant is rejected before any operation is created.
func TestAnswerBatchPromptRefValidation(t *testing.T) {
	inference := stubInference(t)
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     inference,
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	post := func(body string) int {
		resp, err := client.Post("http://unix/1.0/answer/batch", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /1.0/answer/batch: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Both prompt and prompt_ref set → 400.
	if code := post(`{"prompt":"x","prompt_ref":"y","questions":[{"question":"q"}]}`); code != http.StatusBadRequest {
		t.Errorf("prompt+prompt_ref: status = %d, want 400", code)
	}
	// Unknown prompt_ref → 404.
	if code := post(`{"prompt_ref":"nope","questions":[{"question":"q"}]}`); code != http.StatusNotFound {
		t.Errorf("unknown prompt_ref: status = %d, want 404", code)
	}
}
