package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

// maxOutputTokens is the output ceiling requested on every call.
//
// It is set explicitly and generously on purpose. A provider default sized for
// ordinary replies truncates the answer without reducing the reasoning that displaced
// it — Gemini enforces the ceiling including thought tokens — so the fix is a large
// ceiling plus per-call reasoning effort, never a small ceiling.
//
// For scale: the largest manifest in this repository is about 19K tokens in total, so
// the manifest itself was never the constraint. Reasoning is.
const maxOutputTokens = 65536

// triageMaxOutputTokens is triage's own, smaller ceiling.
//
// Triage returns a short report — 374 to 633 answer tokens on the RFPs measured, with
// at most 3,910 of reasoning — so 16K leaves it ample room. The full ceiling bought
// nothing for triage and cost a great deal when it went wrong: a runaway triage on a
// 40-page PDF ran to over 61K answer tokens before stopping. At 16K the same failure
// is reported in a quarter of the time and cost.
const triageMaxOutputTokens = 16384

// geminiBaseURL is the only destination analysis ever calls.
//
// It is a constant, not configuration, and it is passed to the SDK explicitly. Both
// halves matter: an operator-settable endpoint would let analysis be pointed back at
// a gateway, and leaving the SDK to resolve it would let GOOGLE_GEMINI_BASE_URL on the
// host redirect confidential RFP content without anyone noticing.
const geminiBaseURL = "https://generativelanguage.googleapis.com/"

// Client calls Google's Gemini API directly.
//
// It holds the resolved Settings and the API key. Settings is safe to print; the key
// is unexported and never rendered, which is what keeps it out of verbose output,
// error messages and the emitted manifest.
type Client struct {
	settings Settings
	api      *genai.Client
	verbose  bool
}

// resolveKey returns the API key from the one variable it may come from.
func resolveKey() string {
	return strings.TrimSpace(os.Getenv(envGeminiKey))
}

// Validate reports whether analysis can run, collecting every missing prerequisite so
// an operator fixes them in one pass.
func Validate(s Settings) error {
	var reasons []string
	if !s.Enabled {
		reasons = append(reasons, fmt.Sprintf("%s is false — set it to true to allow document content to be sent to the Gemini API", ConfEnabled))
	}
	if s.Model == "" {
		reasons = append(reasons, fmt.Sprintf("%s is not set — set it to a Gemini model id such as gemini-3.1-pro-preview", ConfModel))
	}
	if resolveKey() == "" {
		reason := fmt.Sprintf("no API key — set %s in the environment", envGeminiKey)
		// A leftover from the gateway build of this feature: say so, rather than let
		// the operator hunt for why a key they can see is "missing".
		if strings.TrimSpace(os.Getenv(envRetiredKey)) != "" {
			reason += fmt.Sprintf(" (%s is set, but it is no longer read — rename it to %s)", envRetiredKey, envGeminiKey)
		}
		reasons = append(reasons, reason)
	}
	if len(reasons) > 0 {
		return &NotConfiguredError{Reasons: reasons}
	}
	return nil
}

// NewClient builds a client from resolved settings, reading the API key from the
// environment at this moment rather than carrying it in Settings.
func NewClient(s Settings, verbose bool) (*Client, error) {
	return newClient(s, geminiBaseURL, verbose)
}

// newClient is NewClient with the destination as a parameter. Only tests call it with
// anything other than geminiBaseURL, to reach a local fake server.
func newClient(s Settings, baseURL string, verbose bool) (*Client, error) {
	if err := Validate(s); err != nil {
		return nil, err
	}

	// Every field the SDK would otherwise resolve from the environment is set here,
	// so GOOGLE_API_KEY, GOOGLE_GENAI_USE_VERTEXAI / _ENTERPRISE and
	// GOOGLE_GEMINI_BASE_URL on the host are inert.
	api, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:      resolveKey(),
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{BaseURL: baseURL},
	})
	if err != nil {
		return nil, fmt.Errorf("creating the Gemini client: %w", err)
	}
	return &Client{settings: s, api: api, verbose: verbose}, nil
}

// Settings returns the resolved configuration. Safe to print: it carries no key.
func (c *Client) Settings() Settings { return c.settings }

// Model is the model id every call uses.
func (c *Client) Model() string { return c.settings.Model }

// part is one piece of request content, independent of the SDK so that triage and
// generation do not depend on the wire format. Exactly one of text and pdf is set.
type part struct {
	text string
	pdf  []byte
}

func textPart(s string) part { return part{text: s} }

// thinkingConfig maps a configured effort onto Gemini's thinking level, leaving it
// unset when no effort is configured so the model's own default applies. "none" never
// reaches here: LoadSettings rejects it.
func thinkingConfig(e Effort) *genai.ThinkingConfig {
	var level genai.ThinkingLevel
	switch e {
	case EffortMinimal:
		level = genai.ThinkingLevelMinimal
	case EffortLow:
		level = genai.ThinkingLevelLow
	case EffortMedium:
		level = genai.ThinkingLevelMedium
	case EffortHigh:
		level = genai.ThinkingLevelHigh
	default:
		return nil
	}
	return &genai.ThinkingConfig{ThinkingLevel: level}
}

// blockingFinishReasons are the finish reasons that mean the provider stopped the
// response on policy grounds rather than because it was finished or out of budget.
var blockingFinishReasons = map[genai.FinishReason]bool{
	genai.FinishReasonSafety:            true,
	genai.FinishReasonRecitation:        true,
	genai.FinishReasonLanguage:          true,
	genai.FinishReasonBlocklist:         true,
	genai.FinishReasonProhibitedContent: true,
	genai.FinishReasonSPII:              true,
	genai.FinishReasonImageSafety:       true,
}

// complete sends one schema-constrained call and returns its raw JSON content.
//
// Every response is checked for the failures a schema cannot prevent: stopping at the
// output ceiling, coming back empty, and being blocked. An empty response is never read
// as "the model found nothing" — at the ceiling it is the signature of reasoning having
// consumed the budget, and treating it as an absence would silently produce a manifest
// missing whatever the call was for.
func (c *Client) complete(
	ctx context.Context,
	call string,
	system string,
	parts []part,
	schemaName string,
	schema map[string]any,
	effort Effort,
	ceiling int32,
) ([]byte, error) {
	contents := []*genai.Content{genai.NewContentFromParts(toGenaiParts(parts), genai.RoleUser)}
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(system, genai.RoleUser),
		MaxOutputTokens:   ceiling,
		ResponseMIMEType:  "application/json",
		// Gemini enforces the schema itself. The response is still validated locally
		// by each caller, because a response can be valid JSON against the schema and
		// still be short of what a manifest needs.
		ResponseJsonSchema: schema,
		ThinkingConfig:     thinkingConfig(effort),
	}

	if c.verbose {
		fmt.Printf("analysis: %s call to the Gemini API (model %s, schema %s, reasoning %q, %d content part(s))\n",
			call, c.settings.Model, schemaName, effort, len(parts))
	}

	resp, err := c.api.Models.GenerateContent(ctx, c.settings.Model, contents, cfg)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, classifyRequestError(err, c.settings, call)
	}

	if len(resp.Candidates) == 0 {
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			return nil, &BlockedError{Call: call, Reason: string(resp.PromptFeedback.BlockReason), Prompt: true}
		}
		return nil, &TruncatedError{Call: call, Empty: true, Ceiling: ceiling}
	}

	candidate := resp.Candidates[0]
	finish := candidate.FinishReason
	if blockingFinishReasons[finish] {
		return nil, &BlockedError{Call: call, Reason: string(finish)}
	}

	// Text skips thought parts, so reasoning never reaches the parser. The untrimmed
	// text is kept for a truncation report: a runaway of whitespace is exactly what
	// trimming would erase from the evidence.
	raw := resp.Text()
	content := strings.TrimSpace(raw)

	var thoughts, answer int32
	if u := resp.UsageMetadata; u != nil {
		thoughts, answer = u.ThoughtsTokenCount, u.CandidatesTokenCount
	}
	if c.verbose {
		fmt.Printf("analysis: %s response %d bytes (finish reason %q, %d thought + %d answer tokens of %d)\n",
			call, len(raw), finish, thoughts, answer, ceiling)
	}

	if content == "" {
		// Empty with any finish reason is a failure. When the reason is the ceiling
		// it is reasoning exhaustion; otherwise it is a provider fault. Neither is an
		// answer, so neither may produce a manifest.
		return nil, &TruncatedError{Call: call, Empty: true, ThoughtTokens: thoughts, AnswerTokens: answer, Ceiling: ceiling}
	}
	if finish == genai.FinishReasonMaxTokens {
		return nil, &TruncatedError{Call: call, ThoughtTokens: thoughts, AnswerTokens: answer, Ceiling: ceiling, Content: raw}
	}

	block, err := extractJSONBlock(content)
	if err != nil {
		return nil, &MalformedResponseError{
			Call: call, FinishReason: string(finish), Content: content, Reason: err.Error(),
		}
	}

	// Validity is checked here rather than left to the caller's unmarshal, because
	// this is the only place that still knows the finish reason — and the finish
	// reason is what separates "the response was cut off" from "the model answered
	// with something that is not JSON". A caller reporting only the parser's
	// complaint ("found unexpected end of stream") cannot tell those apart, which
	// makes the failure undiagnosable from the message alone.
	if !json.Valid([]byte(block)) {
		return nil, &MalformedResponseError{
			Call: call, FinishReason: string(finish), Content: block,
			Reason: "the response is not valid JSON",
		}
	}
	return []byte(block), nil
}

// toGenaiParts renders request parts in the SDK's shape. A PDF travels inline as its
// original bytes — never through the Files API, which retains uploads server-side.
func toGenaiParts(parts []part) []*genai.Part {
	out := make([]*genai.Part, 0, len(parts))
	for _, p := range parts {
		if p.pdf != nil {
			out = append(out, genai.NewPartFromBytes(p.pdf, "application/pdf"))
			continue
		}
		out = append(out, genai.NewPartFromText(p.text))
	}
	return out
}

// documentParts renders a conveyance as request parts.
//
// A PDF becomes an inline document part carrying the original bytes, so the model
// applies its own document understanding. A converted document becomes a text part.
// Each is preceded by a label naming its filename and role, so the model knows which
// document it is reading and whether it is the RFP to answer or background material.
func documentParts(conv *Conveyance) []part {
	parts := make([]part, 0, len(conv.Documents)*2)
	for _, doc := range conv.Documents {
		label := fmt.Sprintf("--- %s document: %s (%s) ---", doc.Role, doc.Filename, doc.Format)
		parts = append(parts, textPart(label))

		if doc.PDFBytes != nil {
			parts = append(parts, part{pdf: doc.PDFBytes})
			continue
		}
		parts = append(parts, textPart(doc.Text))
	}
	return parts
}
