package analysis

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/genai"
)

// envGeminiKey is the one environment variable the API key is read from. The key is
// never read from config, and never from the SDK's own environment auto-detection:
// the SDK also honours GOOGLE_API_KEY and gives it precedence, which would make
// rag-cli's behaviour depend on a variable set for an unrelated tool on the same
// machine.
//
// There is deliberately no fallback to the chat backend's key. Analysis always goes
// to Google, and the chat backend is Bedrock or a local server, so a fallback could
// only ever send a credential to a provider it does not belong to.
const envGeminiKey = "GEMINI_API_KEY"

// envRetiredKey is the variable an earlier build of this branch read. It is never
// read now; it is only checked so that a leftover systemd drop-in produces a rename
// hint rather than a bare "no key".
const envRetiredKey = "ANALYSIS_API_KEY"

// NotConfiguredError says analysis cannot run and what is missing. It is a distinct
// type so callers can offer the extraction path instead of reporting a hard failure:
// analysis being unconfigured is an expected state, not a fault.
type NotConfiguredError struct {
	// Reasons lists every missing prerequisite, so an operator fixes them in one
	// pass rather than discovering them one error at a time.
	Reasons []string
}

func (e *NotConfiguredError) Error() string {
	if len(e.Reasons) == 1 {
		return "analysis is not configured: " + e.Reasons[0]
	}
	return "analysis is not configured:\n  - " + strings.Join(e.Reasons, "\n  - ")
}

// IsNotConfigured reports whether err is a NotConfiguredError.
func IsNotConfigured(err error) bool {
	var target *NotConfiguredError
	return errors.As(err, &target)
}

// TruncatedError says a response stopped at the model's output ceiling.
//
// The ceiling covers the model's internal reasoning as well as the visible answer, and
// the two failures it produces need opposite remedies. When reasoning consumed the
// budget, lowering reasoning effort leaves room for the answer. When the answer itself
// filled the budget — a model stuck repeating itself or emitting whitespace under a JSON
// schema, observed on triage at over 60K answer tokens — reasoning is irrelevant and the
// evidence is in the text. So the error carries the token split and an excerpt, and
// chooses its advice from them rather than always blaming reasoning.
type TruncatedError struct {
	Call  string // which generation call truncated
	Empty bool   // true when nothing at all came back

	// ThoughtTokens and AnswerTokens are the provider's usage counts, zero when it
	// reported none. Content is what arrived, for the excerpt.
	ThoughtTokens int32
	AnswerTokens  int32
	Ceiling       int32
	Content       string
}

func (e *TruncatedError) Error() string {
	what := "was truncated"
	if e.Empty {
		what = "came back empty"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "the %s response %s at the model's output ceiling", e.Call, what)
	if e.ThoughtTokens > 0 || e.AnswerTokens > 0 {
		fmt.Fprintf(&b, " (%d reasoning + %d answer tokens of %d)", e.ThoughtTokens, e.AnswerTokens, e.Ceiling)
	}

	if e.AnswerTokens > e.ThoughtTokens {
		fmt.Fprintf(&b, ".\nThe answer itself filled the budget, not the model's reasoning, so changing reasoning "+
			"effort will not help. A %s response is normally far smaller than this; the likeliest cause is a "+
			"runaway response — the model repeating itself or emitting whitespace — which a retry often avoids. "+
			"If it recurs on the same document, keep the excerpt below for diagnosis.", e.Call)
	} else {
		fmt.Fprintf(&b, " — on a reasoning model that ceiling covers the model's own reasoning, so lower %s "+
			"(or %s) to leave room for the answer", ConfReasoningStrategy, ConfReasoningQuestions)
	}
	if e.Content != "" {
		fmt.Fprintf(&b, "\n\n%s", excerptOf(e.Content))
	}
	return b.String()
}

// IsTruncated reports whether err is a TruncatedError.
func IsTruncated(err error) bool {
	var target *TruncatedError
	return errors.As(err, &target)
}

// BlockedError says the provider refused a prompt or a response on safety or policy
// grounds.
//
// It is kept apart from truncation and from a malformed response on purpose, because
// the remedy is different: lowering reasoning effort or retrying does nothing for a
// blocked RFP, and a person has to look at what in the document tripped the filter.
type BlockedError struct {
	Call   string
	Reason string // the provider's block or finish reason, e.g. "SAFETY"
	Prompt bool   // true when the prompt itself was blocked, before any generation
}

func (e *BlockedError) Error() string {
	what := "response"
	if e.Prompt {
		what = "request"
	}
	return fmt.Sprintf(
		"the Gemini API blocked the %s %s (reason %s) — this is a content-policy decision, not a "+
			"budget or format problem, so retrying or lowering reasoning effort will not help; check "+
			"what in the documents could have triggered it",
		e.Call, what, e.Reason)
}

// IsBlocked reports whether err is a BlockedError.
func IsBlocked(err error) bool {
	var target *BlockedError
	return errors.As(err, &target)
}

// IncompleteError says a response was well-formed but does not carry what a manifest
// requires. It is deliberately distinct from a parse failure: a response that parses
// and is merely missing a field is the failure mode a schema does not prevent.
type IncompleteError struct {
	Call    string
	Missing []string
}

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("the %s response is missing %s", e.Call, strings.Join(e.Missing, ", "))
}

// ShortfallError says generation produced materially fewer questions than triage
// estimated. It names both counts, because the whole point is to let a human judge
// whether the shortfall is real.
type ShortfallError struct {
	Expected int
	Produced int
}

func (e *ShortfallError) Error() string {
	return fmt.Sprintf(
		"generation produced %d questions but triage estimated about %d — too few to treat as complete; "+
			"re-run, or lower %s so more of the budget goes to the answer",
		e.Produced, e.Expected, ConfReasoningQuestions)
}

// NotDocumentCapableError says the configured model rejected the conveyed document.
//
// Every text-and-document Gemini model reads PDFs, so this is what a model id that
// exists but is the wrong kind of model looks like — a speech or image model, say.
// Reporting the provider's raw rejection would leave an operator reading vendor
// documentation to work that out.
type NotDocumentCapableError struct {
	Model      string
	Underlying error
}

func (e *NotDocumentCapableError) Error() string {
	return fmt.Sprintf("the Gemini model %q does not accept documents, so it cannot be used for analysis — "+
		"set %s to a text model such as gemini-3.1-pro-preview or gemini-3.8-flash",
		e.Model, ConfModel)
}

func (e *NotDocumentCapableError) Unwrap() error { return e.Underlying }

// IsNotDocumentCapable reports whether err is a NotDocumentCapableError.
func IsNotDocumentCapable(err error) bool {
	var target *NotDocumentCapableError
	return errors.As(err, &target)
}

// ModelNotFoundError says the Gemini API does not know the configured model id.
//
// The predictable cause is an id carried over from a gateway, which namespaces ids
// ("google/gemini-3.1-pro-preview") where the direct API does not, so that case gets
// its own hint.
type ModelNotFoundError struct {
	Model   string
	Message string
}

func (e *ModelNotFoundError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "the Gemini API does not know the model %q, set by %s", e.Model, ConfModel)
	if e.Message != "" {
		fmt.Fprintf(&b, " (%s)", e.Message)
	}
	if strings.Contains(e.Model, "/") {
		b.WriteString(".\nDirect Gemini model ids are unprefixed — for example gemini-3.1-pro-preview " +
			"rather than google/gemini-3.1-pro-preview — so this looks like an id from a gateway")
	}
	return b.String()
}

// IsModelNotFound reports whether err is a ModelNotFoundError.
func IsModelNotFound(err error) bool {
	var target *ModelNotFoundError
	return errors.As(err, &target)
}

// AuthError says the Gemini API rejected the credential. It names the variable the key
// came from; the value is never rendered.
type AuthError struct {
	Model   string
	Status  int
	Message string
}

func (e *AuthError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "the Gemini API rejected the API key from %s (HTTP %d", envGeminiKey, e.Status)
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	b.WriteString(").\nCheck that it is a valid Gemini API key and has not been revoked")
	if e.Status == http.StatusForbidden {
		b.WriteString("; a 403 can also mean the Generative Language API is not enabled on the key's project")
	}
	return b.String()
}

// IsAuth reports whether err is an AuthError.
func IsAuth(err error) bool {
	var target *AuthError
	return errors.As(err, &target)
}

// QuotaError says the key's project hit its rate limit or quota. An unpaid-tier
// project reaches this quickly on a large RFP, which is a second reason — after data
// handling — that the key must belong to a billing-enabled project.
type QuotaError struct {
	Call    string
	Model   string
	Message string
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("the %s request to model %q hit the Gemini API's rate limit or quota (%s) — wait and "+
		"retry, and check that %s belongs to a billing-enabled project", e.Call, e.Model, e.Message, envGeminiKey)
}

// documentRejectionSignals are substrings that mark a 400 as "this model does not take
// documents", as opposed to any other bad request.
//
// Matching on message text is unattractive but unavoidable, since the error carries
// no machine-readable code for it. The list errs towards missing a case rather than
// misclassifying an unrelated failure — a missed case surfaces the provider's own
// message, which is merely less helpful, while a false positive would hide a real
// error. It is to be confirmed against live responses.
var documentRejectionSignals = []string{
	"unsupported mime type",
	"mime type is not supported",
	"does not support document",
	"document input is not supported",
	"inline_data",
	"only supports text",
}

// classifyRequestError turns a provider error into one that says what to do about it,
// and returns it wrapped with the call name when it recognises nothing.
func classifyRequestError(err error, s Settings, call string) error {
	if err == nil {
		return nil
	}

	var apiErr genai.APIError
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("%s request failed: %w", call, err)
	}
	lower := strings.ToLower(apiErr.Message + " " + apiErr.Status + " " + fmt.Sprint(apiErr.Details))

	switch {
	// Gemini reports a malformed or revoked key as 400 INVALID_ARGUMENT with reason
	// API_KEY_INVALID, not 401, so a status-only check would file the most common
	// auth failure as an ordinary bad request.
	case apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusForbidden ||
		(apiErr.Code == http.StatusBadRequest &&
			(strings.Contains(lower, "api_key_invalid") || strings.Contains(lower, "api key not valid"))):
		return &AuthError{Model: s.Model, Status: apiErr.Code, Message: apiErr.Message}

	case apiErr.Code == http.StatusNotFound:
		return &ModelNotFoundError{Model: s.Model, Message: apiErr.Message}

	case apiErr.Code == http.StatusTooManyRequests:
		return &QuotaError{Call: call, Model: s.Model, Message: apiErr.Message}

	case apiErr.Code == http.StatusBadRequest && containsAnyFold(lower, documentRejectionSignals...):
		return &NotDocumentCapableError{Model: s.Model, Underlying: err}
	}
	return fmt.Errorf("%s request failed: %w", call, err)
}

// MalformedResponseError says a response could not be read as JSON, and carries the
// evidence needed to tell why.
//
// The distinction that matters is between a response that was cut off and one that was
// never JSON to begin with, and only the finish reason separates them. A bare parser
// complaint — "found unexpected end of stream" — looks identical in both cases, so this
// error reports the finish reason, the size, and a bounded excerpt from each end of what
// actually arrived. Without those, the operator's only recourse is guesswork.
type MalformedResponseError struct {
	Call         string
	FinishReason string
	Content      string
	Reason       string
}

// excerptLimit is how much of each end of the payload is shown. Enough to recognise the
// shape and see where it stops, short enough not to bury the message.
const excerptLimit = 600

func (e *MalformedResponseError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "the %s response could not be read: %s", e.Call, e.Reason)
	fmt.Fprintf(&b, "\n  finish reason : %q", e.FinishReason)
	fmt.Fprintf(&b, "\n  size          : %d bytes", len(e.Content))

	// The finish reason turns an opaque parse failure into a diagnosis.
	switch {
	case e.looksTruncated():
		fmt.Fprintf(&b, "\n\nThe provider reported that generation stopped at the output ceiling, so the "+
			"response is cut off rather than malformed. On a reasoning model the ceiling covers the "+
			"model's own reasoning, so lower %s (or %s) to leave room for the answer.",
			ConfReasoningQuestions, ConfReasoningStrategy)
	// Trimmed, because a provider that reports a blank reason may send whitespace
	// rather than an empty string, and both mean the same thing: nothing usable.
	case strings.TrimSpace(e.FinishReason) == "" || containsAnyFold(e.FinishReason, "error"):
		fmt.Fprintf(&b, "\n\nThe provider reported no usable finish reason, which usually means the "+
			"upstream stream was interrupted. Retrying is worth one attempt; if it recurs, lower %s so "+
			"the response is shorter.", ConfReasoningQuestions)
	default:
		fmt.Fprintf(&b, "\n\nThe provider reported a normal completion, so the model returned something "+
			"that is not the JSON the schema asked for. Check that %s names a model advertising "+
			"structured-output support.", ConfModel)
	}

	fmt.Fprintf(&b, "\n\n%s", e.excerpt())
	return b.String()
}

// looksTruncated reports whether the finish reason names the output ceiling, across the
// spellings providers and gateways use.
func (e *MalformedResponseError) looksTruncated() bool {
	return containsAnyFold(e.FinishReason, "length", "max_tokens", "maxtokens", "max_output_tokens")
}

// excerpt renders the head and tail of the payload, so the shape is visible and so the
// point at which it stops is visible — which is the part that shows a cut-off.
func (e *MalformedResponseError) excerpt() string { return excerptOf(e.Content) }

// excerptOf renders the head and tail of a payload. Each end is quoted (%q), so that a
// run of whitespace or control characters — invisible when printed raw, and the
// signature of one kind of runaway response — shows up as escapes.
func excerptOf(content string) string {
	if len(content) <= 2*excerptLimit {
		return fmt.Sprintf("response was:\n%q", content)
	}
	return fmt.Sprintf("response began:\n%q\n\n…and ended:\n%q",
		content[:excerptLimit], content[len(content)-excerptLimit:])
}

// IsMalformedResponse reports whether err is a MalformedResponseError.
func IsMalformedResponse(err error) bool {
	var target *MalformedResponseError
	return errors.As(err, &target)
}

func containsAnyFold(haystack string, needles ...string) bool {
	lower := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}
