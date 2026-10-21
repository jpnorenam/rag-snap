// Package analysis generates a complete 'answer batch' manifest from RFP/RFI
// documents using Google's Gemini API, called directly with the operator's own key.
//
// No gateway sits in the request path, and the destination is not configuration: RFP
// content is confidential, and compliance requires that it reach Google and nothing
// else. See client.go for how the destination is pinned against the host environment.
//
// It is a peer of the pattern-based extraction in cmd/cli/basic/rfp, not a
// replacement for it: extraction stays the default build path, and analysis is an
// opt-in mode that additionally produces the RFP-level prompt, the knowledge-base
// and kapa selections, and a populated domains block.
//
// The package deliberately holds no dependency on cmd/cli/basic (which imports it)
// and none on the retrieval or answering path: a manifest it produces is run by
// exactly the same pipeline as a hand-written one.
package analysis

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/config"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// Analysis configuration keys, seeded as `package` keys by the install hook (and by
// post-refresh, for a snap installed before analysis existed) so that a `user` set
// is accepted for each. Exported following the convention of knowledge.ConfKapa*:
// the package that reads a key owns its name.
const (
	// ConfEnabled gates the feature. It defaults to false because analysis sends
	// document content to a third party.
	ConfEnabled = "analysis.enabled"

	// ConfModel is the Gemini model id to call. It has no compiled-in default: the
	// id is versioned, and the Pro tier ships only under a preview identifier, so
	// baking one in ages badly and fails as an opaque provider error.
	ConfModel = "analysis.model"

	// ConfReasoningQuestions and ConfReasoningStrategy set reasoning effort per
	// generation call. They are configuration rather than constants because the
	// right value is tuned against real RFPs, and because the levels a model
	// accepts differ within the Gemini family.
	ConfReasoningQuestions = "analysis.reasoning.questions"
	ConfReasoningStrategy  = "analysis.reasoning.strategy"

	// ConfAnswerWords and ConfAnswerLimit are the interview's starting values.
	ConfAnswerWords = "analysis.answer.words"
	ConfAnswerLimit = "analysis.answer.limit"
)

// DefaultAnswerWords is the interview's answer-length default when
// ConfAnswerWords is unset.
const DefaultAnswerWords = 100

// LengthMode says whether a target answer length may be exceeded.
type LengthMode string

const (
	// LengthHard is a limit that must never be exceeded.
	LengthHard LengthMode = "hard"
	// LengthSoft is a target that may be slightly exceeded.
	LengthSoft LengthMode = "soft"
)

// Valid reports whether m is one of the two defined modes. A length is never
// rendered into a prompt without a mode, so an unrecognised value is rejected
// rather than defaulted.
func (m LengthMode) Valid() bool {
	return m == LengthHard || m == LengthSoft
}

// ParseLengthMode accepts the two mode spellings, case-insensitively.
func ParseLengthMode(s string) (LengthMode, error) {
	m := LengthMode(strings.ToLower(strings.TrimSpace(s)))
	if !m.Valid() {
		return "", fmt.Errorf("answer length mode must be %q or %q, got %q", LengthHard, LengthSoft, s)
	}
	return m, nil
}

// Effort is a reasoning-effort level, mapped onto Gemini's thinking level. The zero
// value means "send no thinking configuration", leaving the model's own default.
type Effort string

// The reasoning-effort levels the analysis.reasoning.* keys accept.
const (
	EffortUnset   Effort = ""
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
)

// NextEffort is the next level up the reasoning ladder, used to re-run an extraction that
// fell short. It returns EffortUnset at the top, where there is nowhere higher to go. An
// unset effort leaves the model at a default rag-cli does not know, so its next step is
// high: the one level guaranteed not to be below that default.
func NextEffort(e Effort) Effort {
	switch e {
	case EffortNone, EffortMinimal:
		return EffortLow
	case EffortLow:
		return EffortMedium
	case EffortMedium, EffortUnset:
		return EffortHigh
	}
	return EffortUnset
}

// ParseEffort accepts the documented spellings, case-insensitively. An empty value
// yields EffortUnset rather than an error, so an operator can leave thinking at the
// model's default.
//
// "none" is a valid spelling but not a usable level: LoadSettings rejects it, because
// no Gemini 3 model can turn reasoning off. "minimal" is accepted here even though the
// Pro tier may refuse it, since that is model-specific and surfaces as the provider's
// own error.
func ParseEffort(s string) (Effort, error) {
	e := Effort(strings.ToLower(strings.TrimSpace(s)))
	switch e {
	case EffortUnset, EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh:
		return e, nil
	}
	return "", fmt.Errorf("reasoning effort must be one of none, minimal, low, medium, high (or empty), got %q", s)
}

// Settings is the resolved analysis configuration.
//
// It deliberately carries no API key and no endpoint. The key is read from the
// environment by NewClient at the moment it is needed, which is what lets Settings be
// printed in verbose output and embedded in diagnostics without disclosing a secret;
// the endpoint is a constant, because it is not something an operator may change.
type Settings struct {
	Enabled bool
	Model   string

	ReasoningQuestions Effort
	ReasoningStrategy  Effort

	AnswerWords int
	AnswerLimit LengthMode
}

// LoadSettings reads every analysis key, applying the documented defaults where a
// key is unset. It does not read the environment and does not validate that analysis
// is usable — call Validate for that, so that a caller wanting only the interview
// defaults (the UI's prefilled form, for instance) is not forced to have a key present.
func LoadSettings(cfg storage.Config) (Settings, error) {
	var s Settings

	s.Enabled = configBool(cfg, ConfEnabled, false)
	s.Model = configString(cfg, ConfModel)

	// A missing model is left for Validate to report rather than failing here, so that
	// an operator with nothing configured yet is told about the model and the key in
	// one pass. A malformed value is still an error, since that is a mistake rather
	// than an absence.
	var err error
	if s.ReasoningQuestions, err = loadEffort(cfg, ConfReasoningQuestions); err != nil {
		return Settings{}, err
	}
	if s.ReasoningStrategy, err = loadEffort(cfg, ConfReasoningStrategy); err != nil {
		return Settings{}, err
	}

	s.AnswerWords = DefaultAnswerWords
	if raw := configString(cfg, ConfAnswerWords); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return Settings{}, fmt.Errorf("config key %q must be a positive whole number of words, got %q", ConfAnswerWords, raw)
		}
		s.AnswerWords = n
	}

	s.AnswerLimit = LengthSoft
	if raw := configString(cfg, ConfAnswerLimit); raw != "" {
		mode, modeErr := ParseLengthMode(raw)
		if modeErr != nil {
			return Settings{}, fmt.Errorf("config key %q: %w", ConfAnswerLimit, modeErr)
		}
		s.AnswerLimit = mode
	}

	return s, nil
}

// loadEffort reads one reasoning-effort key, rejecting "none" here rather than letting
// it reach the provider mid-run: Gemini documents that reasoning cannot be turned off
// for any Gemini 3 model, so the request could only ever fail.
func loadEffort(cfg storage.Config, key string) (Effort, error) {
	e, err := ParseEffort(configString(cfg, key))
	if err != nil {
		return "", fmt.Errorf("config key %q: %w", key, err)
	}
	if e == EffortNone {
		return "", fmt.Errorf("config key %q: %q is not supported — Gemini 3 models cannot disable reasoning; "+
			"use minimal, low, medium or high, or leave it empty for the model's default", key, e)
	}
	return e, nil
}

// configString reads a key as a trimmed string, treating any read error as unset.
// Every analysis key has a documented default or is optional, so a missing key is
// never itself an error here.
func configString(cfg storage.Config, key string) string {
	val, err := config.GetString(cfg, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(val)
}

// configBool reads a key as a boolean, returning fallback when unset or empty.
// It matches the truthiness of cmd/cli/basic.getConfigBool so that an operator sees
// one behaviour across every key.
func configBool(cfg storage.Config, key string, fallback bool) bool {
	val := configString(cfg, key)
	if val == "" {
		return fallback
	}
	return val == "true" || val == "1"
}
