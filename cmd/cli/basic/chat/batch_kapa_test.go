package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// stubInference answers every chat completion with a fixed answer.
func stubInference(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","object":"chat.completion","created":0,"model":"stub-model",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"grounded answer"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// stubKapa serves kapa.ai retrieval, failing any query that contains failOn.
func stubKapa(t *testing.T, failOn string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.Query, failOn) {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"source_url":"https://docs.example/page","content":"kapa content"}]`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func kapaManifest() *BatchManifest {
	return &BatchManifest{
		Version:          "1.0",
		Model:            "stub-model",
		KapaSourceGroups: []string{"group-a"},
		Questions: []BatchQuestion{
			{ID: "Q1", Question: "First question"},
			{ID: "Q2", Question: "Second question FAILKAPA"},
			{ID: "Q3", Question: "Third question"},
		},
	}
}

// TestRunBatchReportsKapaFailureAndContinues covers the kapa-retrieval
// requirement that a kapa.ai request failing for one question is reported
// against that question and does not abort the rest of the batch.
func TestRunBatchReportsKapaFailureAndContinues(t *testing.T) {
	kapa := knowledge.NewKapaClientAt(stubKapa(t, "FAILKAPA"), "proj", "key")

	var warnings []string
	hooks := BatchHooks{OnWarning: func(msg string) { warnings = append(warnings, msg) }}
	out, err := RunBatch(context.Background(), stubInference(t), nil, kapa, "", kapaManifest(), PromptConfig{}, 0.1, hooks, false)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	if len(out.Results) != 3 {
		t.Fatalf("got %d results, want all 3 questions answered", len(out.Results))
	}
	for _, r := range out.Results {
		wantKapa := 1
		switch r.ID {
		case "Q2":
			// No local knowledge and kapa.ai failed: nothing to ground on.
			wantKapa = 0
			if r.Answer != noContextAnswer {
				t.Errorf("Q2 answer = %q, want the no-context answer", r.Answer)
			}
		default:
			if r.Answer != "grounded answer" {
				t.Errorf("%s answer = %q, want the kapa.ai-grounded answer", r.ID, r.Answer)
			}
		}
		if r.Retrieved == nil || r.Retrieved.Kapa != wantKapa || r.Retrieved.Local != 0 {
			t.Errorf("%s retrieved = %+v, want {local 0, kapa %d}", r.ID, r.Retrieved, wantKapa)
		}
	}

	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want exactly one for Q2", warnings)
	}
	if !strings.HasPrefix(warnings[0], "question Q2: kapa.ai retrieval failed") {
		t.Errorf("warning = %q, want it reported against Q2", warnings[0])
	}
}

// TestRunBatchReportsUnconfiguredKapa covers the pre-flight check: groups
// selected with no kapa client is reported once, and the batch still answers.
func TestRunBatchReportsUnconfiguredKapa(t *testing.T) {
	var warnings []string
	hooks := BatchHooks{OnWarning: func(msg string) { warnings = append(warnings, msg) }}
	out, err := RunBatch(context.Background(), stubInference(t), nil, nil, "", kapaManifest(), PromptConfig{}, 0.1, hooks, false)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(out.Results) != 3 {
		t.Fatalf("got %d results, want all 3 questions answered", len(out.Results))
	}
	if len(warnings) != 1 || warnings[0] != kapaUnavailableWarning {
		t.Errorf("warnings = %q, want the single kapa-unavailable warning", warnings)
	}
}

// TestRunBatchNoWarningWithoutKapaSelection keeps a manifest that never asked
// for kapa.ai free of kapa warnings, even when no client is configured.
func TestRunBatchNoWarningWithoutKapaSelection(t *testing.T) {
	manifest := kapaManifest()
	manifest.KapaSourceGroups = nil

	var warnings []string
	hooks := BatchHooks{OnWarning: func(msg string) { warnings = append(warnings, msg) }}
	out, err := RunBatch(context.Background(), stubInference(t), nil, nil, "", manifest, PromptConfig{}, 0.1, hooks, false)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	// No source was active, so no retrieval was attempted and no counts recorded.
	for _, r := range out.Results {
		if r.Retrieved != nil {
			t.Errorf("%s retrieved = %+v, want none recorded", r.ID, r.Retrieved)
		}
	}
}
