package api

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// judgedBatchMeta is the slice of a batch operation's metadata the judge tests read.
type judgedBatchMeta struct {
	Phase            string             `json:"phase"`
	JudgeUnavailable string             `json:"judge_unavailable"`
	Judge            *chat.JudgeSummary `json:"judge"`
	Results          []chat.BatchResult `json:"results"`
}

// runJudgedBatch posts a two-question batch and waits for it. OpenSearch is
// unreachable, so both answers are no-context gaps: they are judged locally, which
// exercises the daemon's judging path end to end without any call to Gemini.
func runJudgedBatch(t *testing.T, configLines []string, judgeField string) judgedBatchMeta {
	t.Helper()
	sock, _ := startTestServerWithConfig(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     stubInference(t),
		backendTika:       "http://127.0.0.1:1",
	}, configLines)
	client := dialSocket(sock)

	body := `{` + judgeField + `"questions": [{"id": "q1", "question": "One?"}, {"id": "q2", "question": "Two?"}]}`
	resp, err := client.Post("http://unix/1.0/answer/batch", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/answer/batch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d; body=%s", resp.StatusCode, b)
	}
	var env struct {
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}

	wresp, err := client.Get("http://unix" + env.Operation + "/wait?timeout=10")
	if err != nil {
		t.Fatal(err)
	}
	defer wresp.Body.Close()
	var wait struct {
		Metadata struct {
			StatusCode int             `json:"status_code"`
			Err        string          `json:"err"`
			Metadata   judgedBatchMeta `json:"metadata"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(wresp.Body).Decode(&wait); err != nil {
		t.Fatal(err)
	}
	if wait.Metadata.StatusCode != statusCodeSuccess {
		t.Fatalf("operation status = %d (%s); judging must never fail a batch", wait.Metadata.StatusCode, wait.Metadata.Err)
	}
	return wait.Metadata.Metadata
}

var judgeConfigured = []string{"judge.model=gemini-test"}

func TestAnswerBatchIsJudgedByDefault(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	meta := runJudgedBatch(t, judgeConfigured, "")

	if meta.Judge == nil || meta.Judge.Model != "gemini-test" || meta.Judge.Red != 2 {
		t.Fatalf("judge summary = %+v, want two red gaps judged by gemini-test", meta.Judge)
	}
	for _, r := range meta.Results {
		if r.Judgment == nil || r.Judgment.Rating != chat.RatingRed || !slices.Contains(r.Judgment.Issues, chat.IssueGap) {
			t.Errorf("result %s judgment = %+v, want a red gap", r.ID, r.Judgment)
		}
	}
	if meta.Phase != "done" {
		t.Errorf("phase = %q, want done", meta.Phase)
	}
}

func TestAnswerBatchJudgeFalseOptsOut(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	meta := runJudgedBatch(t, judgeConfigured, `"judge": false, `)
	if meta.Judge != nil {
		t.Errorf("judge summary = %+v, want none", meta.Judge)
	}
	for _, r := range meta.Results {
		if r.Judgment != nil {
			t.Errorf("result %s carries a judgment despite judge:false", r.ID)
		}
	}
}

func TestAnswerBatchReportsWhyItIsNotJudged(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	meta := runJudgedBatch(t, judgeConfigured, "")
	if !strings.Contains(meta.JudgeUnavailable, "GEMINI_API_KEY") {
		t.Errorf("judge_unavailable = %q, want it to name the missing key", meta.JudgeUnavailable)
	}
	if meta.Judge != nil || len(meta.Results) != 2 {
		t.Errorf("an unjudgeable run must still answer, unjudged: %+v", meta)
	}
}

func TestAnswerBatchJudgeDisabledIsSilent(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	meta := runJudgedBatch(t, append([]string{"judge.enabled=false"}, judgeConfigured...), "")
	if meta.JudgeUnavailable != "" || meta.Judge != nil {
		t.Errorf("a judge turned off is a choice, not a fault: %+v", meta)
	}
}

func TestJudgeCapabilityIsAdvertisedOnlyWhenUsable(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	_, srv := startTestServerWithConfig(t, map[string]string{}, judgeConfigured)
	if !slices.Contains(srv.extensions(), "answer_judge") {
		t.Error("answer_judge should be advertised when judging is configured")
	}

	t.Setenv("GEMINI_API_KEY", "")
	if slices.Contains(srv.extensions(), "answer_judge") {
		t.Error("answer_judge must not be advertised without GEMINI_API_KEY")
	}
}
