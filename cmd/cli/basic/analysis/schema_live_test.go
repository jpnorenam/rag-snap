//go:build live

// Live checks against the real Gemini API. Excluded from normal builds; run with
//
//	GEMINI_API_KEY=... go test -tags live -run Live -v ./cmd/cli/basic/analysis/
//
// Each call sends a tiny prompt with low reasoning, so a run costs a few cents. It exists
// because Gemini accepts a narrower JSON Schema subset than the specification, and a
// schema it rejects fails every analysis with a bare 400 — which only the live API can
// show. Set ANALYSIS_LIVE_MODEL to test a model other than gemini-3.1-pro-preview.
package analysis

import (
	"context"
	"os"
	"testing"
	"time"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv(envGeminiKey) == "" {
		t.Skip(envGeminiKey + " is not set")
	}
	model := os.Getenv("ANALYSIS_LIVE_MODEL")
	if model == "" {
		model = "gemini-3.1-pro-preview"
	}
	c, err := NewClient(Settings{Enabled: true, Model: model}, true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// liveSchema sends one schema with a minimal prompt and reports whether Gemini accepted it.
func liveSchema(t *testing.T, c *Client, name string, schema map[string]any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, _, err := c.complete(ctx, name, "Return a minimal, valid example object for the schema.",
		[]part{textPart("A two-section compliance matrix with ids C1-C3 and D1-D2.")},
		name, schema, EffortLow, 8192)
	return err
}

func TestLiveSchemasAreAccepted(t *testing.T) {
	c := liveClient(t)
	for name, schema := range map[string]map[string]any{
		"rfp_triage":    triageSchema(),
		"rfp_questions": questionsSchema(),
		"rfp_strategy":  strategySchema(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := liveSchema(t, c, name, schema); err != nil {
				t.Errorf("Gemini rejected the %s schema: %v", name, err)
			}
		})
	}
}

// TestLiveTriageSchemaVariants isolates which triage-schema constraint Gemini rejects,
// by sending the schema with each one removed. Diagnostic: it logs rather than fails.
func TestLiveTriageSchemaVariants(t *testing.T) {
	c := liveClient(t)
	sectionsOf := func(s map[string]any) map[string]any {
		return s["properties"].(map[string]any)["sections"].(map[string]any)
	}
	variants := map[string]func(map[string]any){
		"as shipped":           func(map[string]any) {},
		"without maxItems":     func(s map[string]any) { delete(sectionsOf(s), "maxItems") },
		"without new required": func(s map[string]any) { sectionsOf(s)["items"].(map[string]any)["required"] = []any{"name"} },
		"without either": func(s map[string]any) {
			delete(sectionsOf(s), "maxItems")
			sectionsOf(s)["items"].(map[string]any)["required"] = []any{"name"}
		},
	}
	for name, mutate := range variants {
		s := triageSchema()
		mutate(s)
		if err := liveSchema(t, c, "rfp_triage", s); err != nil {
			t.Logf("%-20s REJECTED: %v", name, err)
		} else {
			t.Logf("%-20s accepted", name)
		}
	}
}
