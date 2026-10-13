package analysis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// mapConfig is a read-only, single-layer storage.Config over a map. Analysis only
// ever reads config, so the layering the real backend applies is irrelevant here —
// what matters is that an absent key reads as unset.
type mapConfig map[string]any

func (c mapConfig) Get(key string) (map[string]any, error) {
	out := map[string]any{}
	if v, ok := c[key]; ok {
		out[key] = v
	}
	return out, nil
}

func (c mapConfig) GetAll() (map[string]any, error) { return map[string]any(c), nil }

func (c mapConfig) GetAllFromLayer(storage.ConfigType) (map[string]any, error) {
	return map[string]any(c), nil
}

func (c mapConfig) Set(string, string, storage.ConfigType) error      { return nil }
func (c mapConfig) SetDocument(string, any, storage.ConfigType) error { return nil }
func (c mapConfig) Unset(string, storage.ConfigType) error            { return nil }

func TestLoadSettingsDefersMissingPrerequisitesToValidate(t *testing.T) {
	// With nothing configured at all, LoadSettings must still succeed so that Validate
	// can report the model and the key together. Failing here would make an operator
	// fix one prerequisite, re-run, discover the next, and repeat.
	s, err := LoadSettings(mapConfig{})
	if err != nil {
		t.Fatalf("LoadSettings with nothing configured should not fail: %v", err)
	}

	t.Setenv(envGeminiKey, "")
	t.Setenv(envRetiredKey, "")
	verr := Validate(s)
	if verr == nil {
		t.Fatal("Validate should reject settings with no model or key")
	}
	for _, want := range []string{ConfEnabled, ConfModel, envGeminiKey} {
		if !strings.Contains(verr.Error(), want) {
			t.Errorf("the combined report should mention %q, got: %v", want, verr)
		}
	}
}

func TestLoadSettingsStillRejectsAMalformedValue(t *testing.T) {
	// An absence is deferred; a mistake is not.
	if _, err := LoadSettings(mapConfig{ConfAnswerWords: "lots"}); err == nil {
		t.Fatal("a malformed value should still fail at load time")
	}
}

func TestLoadSettingsAppliesDocumentedDefaults(t *testing.T) {
	s, err := LoadSettings(mapConfig{})
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Enabled {
		t.Error("analysis should be disabled by default")
	}
	if s.AnswerWords != DefaultAnswerWords {
		t.Errorf("AnswerWords = %d, want %d", s.AnswerWords, DefaultAnswerWords)
	}
	if s.AnswerLimit != LengthSoft {
		t.Errorf("AnswerLimit = %q, want %q", s.AnswerLimit, LengthSoft)
	}
	// Reasoning effort is unset rather than guessed when the key is absent, so the
	// model's own default applies.
	if s.ReasoningQuestions != EffortUnset || s.ReasoningStrategy != EffortUnset {
		t.Errorf("reasoning efforts = %q/%q, want both unset", s.ReasoningQuestions, s.ReasoningStrategy)
	}
}

func TestLoadSettingsReadsSeededValues(t *testing.T) {
	// The values the install hook seeds.
	cfg := mapConfig{
		ConfEnabled:            "true",
		ConfModel:              "gemini-3.1-pro-preview",
		ConfReasoningQuestions: "low",
		ConfReasoningStrategy:  "high",
		ConfAnswerWords:        "120",
		ConfAnswerLimit:        "hard",
	}

	s, err := LoadSettings(cfg)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !s.Enabled {
		t.Error("Enabled should be true")
	}
	if s.Model != "gemini-3.1-pro-preview" {
		t.Errorf("Model = %q", s.Model)
	}
	if s.ReasoningQuestions != EffortLow {
		t.Errorf("ReasoningQuestions = %q, want %q", s.ReasoningQuestions, EffortLow)
	}
	if s.ReasoningStrategy != EffortHigh {
		t.Errorf("ReasoningStrategy = %q, want %q", s.ReasoningStrategy, EffortHigh)
	}
	if s.AnswerWords != 120 {
		t.Errorf("AnswerWords = %d, want 120", s.AnswerWords)
	}
	if s.AnswerLimit != LengthHard {
		t.Errorf("AnswerLimit = %q, want %q", s.AnswerLimit, LengthHard)
	}
}

func TestLoadSettingsRejectsBadValues(t *testing.T) {
	base := func() mapConfig {
		return mapConfig{}
	}

	tests := []struct {
		name string
		key  string
		val  string
	}{
		{"non-numeric words", ConfAnswerWords, "lots"},
		{"zero words", ConfAnswerWords, "0"},
		{"negative words", ConfAnswerWords, "-5"},
		{"unknown length mode", ConfAnswerLimit, "approximate"},
		{"unknown reasoning effort", ConfReasoningStrategy, "maximum"},
		// No Gemini 3 model can turn reasoning off, so "none" could only fail later,
		// mid-run, as a provider rejection.
		{"reasoning disabled for strategy", ConfReasoningStrategy, "none"},
		{"reasoning disabled for questions", ConfReasoningQuestions, "none"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			cfg[tc.key] = tc.val
			if _, err := LoadSettings(cfg); err == nil {
				t.Fatalf("expected an error for %s=%q", tc.key, tc.val)
			} else if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error should name the offending key %q, got: %v", tc.key, err)
			}
		})
	}
}

func TestSettingsCarriesNoAPIKeyField(t *testing.T) {
	// Settings is printed in verbose output and embedded in diagnostics, so the key
	// must not be reachable through it. This guards the shape, not a value: a
	// future field called anything key-like should fail here and be moved.
	s := Settings{Enabled: true, Model: "gemini-3.1-pro-preview", ReasoningStrategy: EffortHigh}
	// %+v names every field, which is exactly what we want to inspect.
	rendered := strings.ToLower(fmt.Sprintf("%+v", s))
	for _, banned := range []string{"apikey", "secret", "token", "password"} {
		if strings.Contains(rendered, banned) {
			t.Errorf("Settings rendering contains %q, which risks disclosing a secret: %s", banned, rendered)
		}
	}
}
