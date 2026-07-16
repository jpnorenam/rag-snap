package api

import (
	"encoding/json"
	"io"
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
