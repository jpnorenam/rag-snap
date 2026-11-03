package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"gopkg.in/yaml.v3"
)

// KeywordList is a YAML-flexible keyword field that accepts either a
// comma-separated string ("kw1, kw2") or a YAML sequence ([kw1, kw2]).
type KeywordList []string

func (kl *KeywordList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*kl = splitKeywords(value.Value)
		return nil
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		// Trim whitespace from each item.
		out := make([]string, 0, len(items))
		for _, item := range items {
			if kw := strings.TrimSpace(item); kw != "" {
				out = append(out, kw)
			}
		}
		*kl = out
		return nil
	default:
		return fmt.Errorf("keywords must be a string or list, got node kind %d", value.Kind)
	}
}

// splitKeywords splits a comma-separated keyword string into trimmed, non-empty tokens.
func splitKeywords(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if kw := strings.TrimSpace(p); kw != "" {
			out = append(out, kw)
		}
	}
	return out
}

// mergeKeywords places the leading keyword lists first (higher priority), in the
// order they are given, then appends generated keywords that are not already
// present, deduplicating case-insensitively across all tiers and keeping each
// term at its first occurrence.
// generated is a space-separated keyword string from rewriteSearchQuery.
// leading holds the higher-priority tiers, highest first: the question's own
// manifest keywords, then any resolved domain's keywords.
// Returns a space-separated string ready for use as a lexical search query.
func mergeKeywords(generated string, leading ...[]string) string {
	total := 0
	for _, list := range leading {
		total += len(list)
	}
	if total == 0 {
		return generated
	}

	// Leading tiers lead; build the seen set from them first.
	genFields := strings.Fields(generated)
	seen := make(map[string]struct{}, total+len(genFields))
	merged := make([]string, 0, total+len(genFields))
	add := func(kw string) {
		lower := strings.ToLower(kw)
		if _, exists := seen[lower]; exists {
			return
		}
		seen[lower] = struct{}{}
		merged = append(merged, kw)
	}

	for _, list := range leading {
		for _, kw := range list {
			add(kw)
		}
	}

	// Append generated keywords not already covered by a leading tier.
	for _, kw := range genFields {
		add(kw)
	}

	return strings.Join(merged, " ")
}

// BatchQuestion describes a single Q&A task within a batch manifest.
type BatchQuestion struct {
	ID       string `yaml:"id,omitempty"`
	Question string `yaml:"question"`
	// Source names where the question came from in the extracted document (a
	// sheet or section name). It is the fallback domain match key for questions
	// whose ID is a bare sequence number, which carries no domain prefix.
	Source   string      `yaml:"source,omitempty"`
	Keywords KeywordList `yaml:"keywords,omitempty"`
}

// Domain describes a requirement domain within a batch: a glob matched against a
// question's ID (or, for bare numeric IDs, its Source), the prose Context
// injected into that question's turn, and optional Keywords the question
// inherits for retrieval. An entry must carry at least one of Context and
// Keywords.
type Domain struct {
	Match    string      `yaml:"match"`
	Context  string      `yaml:"context,omitempty"`
	Keywords KeywordList `yaml:"keywords,omitempty"`
}

// BatchManifest is the top-level structure of a batch chat YAML file.
type BatchManifest struct {
	Version          string   `yaml:"version"`
	Model            string   `yaml:"model,omitempty"`
	KnowledgeBases   []string `yaml:"knowledge_bases,omitempty"`
	KapaSourceGroups []string `yaml:"kapa_source_groups,omitempty"`
	Prompt           string   `yaml:"prompt,omitempty"`
	// PromptRef names a stored answer_system_prompt variant to run this batch on
	// (resolved by the daemon). It is mutually exclusive with the inline Prompt
	// and requires a running daemon; a daemonless run with PromptRef set is an
	// error. Unlike Prompt, it replaces the answer system prompt outright rather
	// than being combined with the source rules.
	PromptRef string `yaml:"prompt_ref,omitempty"`
	// Domains optionally routes questions to requirement domains by ID. It is
	// resolved once per question before answering; see CompileDomains. A manifest
	// without it behaves exactly as one did before domain routing existed.
	Domains   []Domain        `yaml:"domains,omitempty"`
	Questions []BatchQuestion `yaml:"questions"`
}

// BatchResult holds the answer for a single question.
type BatchResult struct {
	ID       string `json:"id,omitempty"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	// Domain records which domains entry was applied, identified by its matched
	// pattern. Empty when the question resolved to no domain. It is the routing
	// audit trail: which rule produced this answer.
	Domain string `json:"domain,omitempty"`
	// Judgment is the judge's verdict on this answer. Absent when the run was not
	// judged, so an unjudged results file keeps the shape it always had.
	Judgment *Judgment `json:"judgment,omitempty"`
}

// BatchOutput is the structured result of a batch run: the resolved model, a
// generation timestamp, and the per-question answers. It is the same shape the
// CLI writes to its JSON results file and the daemon exposes over the API.
type BatchOutput struct {
	GeneratedAt string `json:"generated_at"`
	Model       string `json:"model"`
	// Prompt records which answer_system_prompt resolution drove the run, as a
	// "variant@version" reference (empty for the built-in default or an inline
	// custom prompt). It is the RFP audit trail: which prompt produced these
	// answers.
	Prompt string `json:"prompt,omitempty"`
	// Judge summarises the judging pass. Absent when the run was not judged.
	Judge   *JudgeSummary `json:"judge,omitempty"`
	Results []BatchResult `json:"results"`
}

// errEmptyAnswer is the failure RunBatch reports for a question the model answered
// with no text.
var errEmptyAnswer = errors.New("the model returned an empty answer (it may have spent its output budget on reasoning)")

// noContextAnswer is the fixed response emitted when retrieval returns nothing,
// so the model never answers an ungrounded question from parametric knowledge.
const noContextAnswer = "The provided context does not contain enough information to answer this question."

// BatchHooks observe a batch run for presentation/progress without coupling the
// core to a transport. Each hook is optional; nil hooks are skipped. OnStart
// fires before a question is answered, OnResult after it is answered, and
// OnError when a question fails and is skipped (i is 0-based; total is the
// question count).
//
// On a judged run, OnAnswered fires once every question has been answered and
// before judging finishes, carrying the answers without verdicts, so a caller can
// save them before the judge has a chance to fail. OnJudged fires as each verdict
// lands: idx is the result's index in the output, done counts verdicts so far and
// total the answers the run is expected to produce (the question count, less the
// questions that failed so far). Judge hooks are never called concurrently with each
// other, but they are called concurrently with the answering hooks, and never after
// RunBatch has returned.
type BatchHooks struct {
	OnStart    func(i, total int, q BatchQuestion)
	OnResult   func(i, total int, result BatchResult)
	OnError    func(i, total int, q BatchQuestion, err error)
	OnAnswered func(out *BatchOutput)
	OnJudged   func(idx int, j Judgment, done, total int)
}

func (h BatchHooks) start(i, total int, q BatchQuestion) {
	if h.OnStart != nil {
		h.OnStart(i, total, q)
	}
}

func (h BatchHooks) result(i, total int, r BatchResult) {
	if h.OnResult != nil {
		h.OnResult(i, total, r)
	}
}

func (h BatchHooks) failure(i, total int, q BatchQuestion, err error) {
	if h.OnError != nil {
		h.OnError(i, total, q, err)
	}
}

func (h BatchHooks) answered(out *BatchOutput) {
	if h.OnAnswered != nil {
		h.OnAnswered(out)
	}
}

func (h BatchHooks) judged(idx int, j Judgment, done, total int) {
	if h.OnJudged != nil {
		h.OnJudged(idx, j, done, total)
	}
}

// batchSystemPrompt resolves the system prompt for a batch run. It is built once
// and sent verbatim with every question, so it stays a stable cacheable prefix
// across the run — per-question scoping such as the resolved requirement domain
// goes in the turn instead (see buildRAGPrompt).
func batchSystemPrompt(manifest *BatchManifest, prompts PromptConfig) string {
	if manifest.Prompt == "" {
		return prompts.AnswerSystemPrompt
	}
	// Append the non-negotiable source rules so custom prompts cannot
	// accidentally bypass [CANONICAL]/[UPSTREAM] grounding behaviour.
	return manifest.Prompt + "\n\n" + prompts.SourceRules
}

// LoadBatchManifest reads and parses a batch chat YAML manifest file.
func LoadBatchManifest(path string) (*BatchManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest file: %w", err)
	}
	var manifest BatchManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest yaml: %w", err)
	}
	if len(manifest.Questions) == 0 {
		return nil, fmt.Errorf("manifest contains no questions")
	}
	for i, q := range manifest.Questions {
		if q.Question == "" {
			return nil, fmt.Errorf("question %d has an empty question field", i+1)
		}
	}
	// RunBatch compiles the domains list too; doing it here as well means the CLI
	// reports a malformed block at load time, before connecting to OpenSearch or
	// the inference server.
	if _, err := CompileDomains(manifest.Domains); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// batchRetrieve is the retrieval step RunBatch grounds each question on. It is a
// variable only so tests can decide which questions find context without a live
// knowledge backend.
var batchRetrieve = retrieveContext

// RunBatch runs each question in the manifest through the RAG+LLM pipeline and
// returns the structured results. It is presentation-free: progress is reported
// through hooks rather than printed, and nothing is written to disk, so both the
// CLI and the daemon can drive it. The run honours ctx cancellation between
// questions. When a question retrieves no grounding context, its answer is the
// fixed no-context response rather than an ungrounded generation.
//
// When judge is non-nil, each answer is judged as soon as it is produced, against
// the context it was grounded on, while later questions are still being answered.
// That context is held only until its verdict lands and is never returned. Judging
// never fails the run: a verdict that cannot be had marks its row not judged.
func RunBatch(
	ctx context.Context,
	backend BackendConfig,
	knowledgeClient *knowledge.OpenSearchClient,
	kapaClient *knowledge.KapaClient,
	embeddingModelID string,
	manifest *BatchManifest,
	prompts PromptConfig,
	temperature float64,
	judge *BatchJudge,
	hooks BatchHooks,
	verbose bool,
) (*BatchOutput, error) {
	// Compile the routing table before anything else, so a malformed block fails
	// the run rather than being discovered partway through it. Every caller —
	// the CLI's direct path, the daemon, any future one — validates here.
	domains, err := CompileDomains(manifest.Domains)
	if err != nil {
		return nil, err
	}

	client, err := NewBackend(backend)
	if err != nil {
		return nil, err
	}

	modelName := manifest.Model
	if modelName == "" {
		modelName, err = findModelName(client, verbose)
		if err != nil {
			return nil, fmt.Errorf("resolving model name: %w", err)
		}
	}
	if notice := temperatureNotice(backend.API, temperature); notice != "" && verbose {
		fmt.Println(notice)
	}

	activeIndexes := []string{knowledge.DefaultIndexName()}
	if len(manifest.KnowledgeBases) > 0 {
		activeIndexes = make([]string, len(manifest.KnowledgeBases))
		for i, kb := range manifest.KnowledgeBases {
			activeIndexes[i] = knowledge.FullIndexName(kb)
		}
	}

	session := &Session{
		KnowledgeClient:  knowledgeClient,
		KapaClient:       kapaClient,
		EmbeddingModelID: embeddingModelID,
		ActiveIndexes:    activeIndexes,
		ActiveKapaGroups: manifest.KapaSourceGroups,
	}

	defaultSystemPrompt := batchSystemPrompt(manifest, prompts)

	total := len(manifest.Questions)
	results := make([]BatchResult, 0, total)

	var pool *judgePool
	if judge != nil && judge.Judge != nil {
		pool = newJudgePool(ctx, judge, hooks, total)
	}
	// cancelled returns ctx's error once every judge call in flight has returned, so no
	// judge hook can fire after RunBatch has.
	cancelled := func(err error) (*BatchOutput, error) {
		if pool != nil {
			pool.wait()
		}
		return nil, err
	}

	for i, q := range manifest.Questions {
		if err := ctx.Err(); err != nil {
			return cancelled(err)
		}
		hooks.start(i, total, q)

		// Resolve the requirement domain once per question: it scopes the turn,
		// contributes retrieval keywords, and is recorded on the result.
		var domainContext, domainMatch string
		var domainKeywords []string
		if domain := domains.Resolve(q.ID, q.Source); domain != nil {
			domainContext = domain.Context
			domainMatch = domain.Match
			domainKeywords = domain.Keywords
		}

		// nil history: each question is extracted in isolation, with no prior conversation context.
		lexicalQuery := rewriteSearchQuery(client, modelName, nil, q.Question, verbose)
		// Manifest keywords lead in the lexical query (higher BM25 priority),
		// then the resolved domain's, then the generated ones.
		lexicalQuery = mergeKeywords(lexicalQuery, q.Keywords, domainKeywords)

		// Use the full merged lexical query (manifest + generated keywords) to
		// steer the vector search as well. This ensures that user-provided keywords
		// like "magnum" influence embedding similarity, not just BM25 scoring.
		semanticQuery := q.Question
		if lexicalQuery != q.Question {
			semanticQuery = q.Question + " " + lexicalQuery
		}
		ragContext := batchRetrieve(session, semanticQuery, lexicalQuery, verbose)

		// When no context was retrieved there is nothing to ground the answer on.
		// Skip the LLM call entirely and emit the fixed no-answer string to avoid
		// the model hallucinating from parametric knowledge.
		if ragContext == "" {
			result := BatchResult{ID: q.ID, Question: q.Question, Answer: noContextAnswer, Domain: domainMatch}
			results = append(results, result)
			hooks.result(i, total, result)
			if pool != nil {
				pool.record(len(results)-1, gapJudgment())
			}
			continue
		}

		content, err := client.Complete(ctx, Request{
			Model:       modelName,
			System:      defaultSystemPrompt,
			Messages:    []Message{{Role: RoleUser, Content: buildRAGPrompt(ragContext, domainContext, q.ID, q.Question)}},
			Temperature: &temperature,
			Purpose:     PurposeAnswer,
		})
		if err != nil {
			// A cancelled context surfaces as a request error; propagate it so
			// the operation is recorded as cancelled rather than silently
			// dropping the remaining questions.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return cancelled(ctxErr)
			}
			// A single question failing is non-fatal: report it and skip,
			// matching the CLI's original resilience. A refusal or an output
			// ceiling hit with no text lands here too, never as an empty answer.
			hooks.failure(i, total, q, err)
			if pool != nil {
				pool.skip()
			}
			continue
		}

		answer := StripThinkTags(content)
		// A backend may return no text without an error — the OpenAI route does when
		// a reasoning model spends its whole budget. A blank cell is a failed question,
		// not an answer, and is not worth a judge call.
		if strings.TrimSpace(answer) == "" {
			hooks.failure(i, total, q, errEmptyAnswer)
			if pool != nil {
				pool.skip()
			}
			continue
		}

		result := BatchResult{ID: q.ID, Question: q.Question, Answer: answer, Domain: domainMatch}
		results = append(results, result)
		hooks.result(i, total, result)
		if pool != nil {
			opener, body := splitOpener(answer)
			pool.submit(len(results)-1, JudgeInput{
				Row:           SpreadsheetRow(len(results) - 1),
				ID:            q.ID,
				Question:      q.Question,
				Answer:        answer,
				DomainContext: domainContext,
				SystemPrompt:  defaultSystemPrompt,
				Evidence:      ragContext,
				Words:         countWords(answer),
				Sentences:     countSentences(body),
				Opener:        opener,
			})
		}
	}

	out := &BatchOutput{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Model:       modelName,
		Results:     results,
	}
	if pool == nil {
		return out, nil
	}

	// Hand the caller the answers before waiting on the judge, so a judge that
	// hangs or crashes the process can never cost the answers.
	answered := *out
	answered.Results = append([]BatchResult(nil), results...)
	hooks.answered(&answered)

	verdicts := pool.wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for idx, j := range verdicts {
		out.Results[idx].Judgment = &j
	}
	out.Judge = summariseWithConsistency(ctx, judge.Judge, defaultSystemPrompt, out.Results, pool.unusableErr())
	return out, nil
}

// summariseWithConsistency runs the cross-answer consistency pass over the judged
// answers, merges its findings, and returns the run summary. A failed pass keeps the
// per-answer verdicts untouched and is reported in the summary. When unusable is set
// the judge is known not to work, so the pass is not attempted.
func summariseWithConsistency(ctx context.Context, judge AnswerJudge, systemPrompt string, results []BatchResult, unusable error) *JudgeSummary {
	var inputs []ConsistencyInput
	for i, r := range results {
		// Gaps carry nothing to compare, and a row the judge could not assess would
		// let a finding rest on a verdict the reviewer never sees.
		if isGapAnswer(r.Answer) || r.Judgment == nil || !r.Judgment.Judged() ||
			slices.Contains(r.Judgment.Issues, IssueGap) {
			continue
		}
		inputs = append(inputs, ConsistencyInput{Row: SpreadsheetRow(i), ID: r.ID, Question: r.Question, Answer: r.Answer})
	}

	var (
		consistencyErr error
		kept           []ConsistencyFinding
		discarded      int
	)
	if unusable != nil {
		consistencyErr = unusable
	} else if len(inputs) > 1 {
		report, err := judge.Consistency(ctx, systemPrompt, inputs)
		if err != nil {
			consistencyErr = err
		} else {
			var dropped int
			kept, dropped = applyConsistency(results, report.Findings)
			discarded = report.Discarded + dropped
		}
	}

	s := summarise(judge.Model(), results)
	s.Consistency = consistencyErr == nil
	if consistencyErr != nil {
		s.ConsistencyError = consistencyErr.Error()
	}
	s.ConsistencyFindings = kept
	s.ConsistencyDiscarded = discarded
	return s
}

// ProcessBatchChat runs each question in the manifest through the RAG+LLM pipeline,
// prints Q&A pairs to the terminal, and writes all results to a timestamped JSON file.
//
// A judged run writes the file twice: once with the answers as soon as answering ends,
// then again, atomically, with the verdicts. Whatever happens to the judge, the
// answers are on disk.
func ProcessBatchChat(
	backend BackendConfig,
	knowledgeClient *knowledge.OpenSearchClient,
	kapaClient *knowledge.KapaClient,
	embeddingModelID string,
	manifest *BatchManifest,
	prompts PromptConfig,
	temperature float64,
	judge *BatchJudge,
	verbose bool,
) error {
	fmt.Printf("Found %d questions in batch manifest version %s\n", len(manifest.Questions), manifest.Version)

	filename := fmt.Sprintf("batch-results-%s.json", time.Now().Format("20060102-150405"))
	answersSaved := false

	// The judging spinner waits for answering to end: verdicts arrive while the Q&A is
	// still being printed, and a spinner redrawing the line would garble it. Judge
	// hooks run on judge goroutines, so the spinner state is guarded.
	var (
		judgeMu                 sync.Mutex
		answering               = true
		judgedDone, judgedTotal int
		update                  func(string)
		stopJudging             func()
	)
	judgeLabel := func() string {
		if judgedTotal == 0 {
			return "Judging answers"
		}
		return fmt.Sprintf("Judging answers %d/%d", judgedDone, judgedTotal)
	}

	hooks := BatchHooks{
		OnStart: func(i, total int, q BatchQuestion) {
			fmt.Printf("[%d/%d] Question: %s\n", i+1, total, q.Question)
		},
		OnResult: func(_, _ int, r BatchResult) {
			fmt.Printf("Answer: %s\n---\n", r.Answer)
		},
		OnError: func(i, _ int, _ BatchQuestion, err error) {
			fmt.Printf("error on question %d: %v\n", i+1, err)
		},
		OnAnswered: func(out *BatchOutput) {
			if len(out.Results) > 0 {
				if err := WriteBatchResults(filename, out); err != nil {
					fmt.Printf("warning: could not save the answers before judging: %v\n", err)
				} else {
					answersSaved = true
					fmt.Printf("\nAnswers saved to %s; judging them now.\n", filename)
				}
			}
			judgeMu.Lock()
			defer judgeMu.Unlock()
			answering = false
			// No verdict yet (judgedTotal 0) still means judging is under way.
			if judgedTotal == 0 || judgedDone < judgedTotal {
				update, stopJudging = common.StartUpdatableSpinner(judgeLabel())
			}
		},
	}
	if judge != nil {
		hooks.OnJudged = func(_ int, _ Judgment, done, total int) {
			judgeMu.Lock()
			defer judgeMu.Unlock()
			judgedDone, judgedTotal = done, total
			if !answering && update != nil {
				update(judgeLabel())
			}
		}
	}

	out, err := RunBatch(context.Background(), backend, knowledgeClient, kapaClient, embeddingModelID, manifest, prompts, temperature, judge, hooks, verbose)
	// RunBatch never calls a hook after returning, so the spinner state is settled.
	if stopJudging != nil {
		stopJudging()
	}
	if err != nil {
		if answersSaved {
			return fmt.Errorf("%w (the answers are saved in %s)", err, filename)
		}
		return err
	}
	if len(out.Results) == 0 {
		return fmt.Errorf("all questions failed; no results to write")
	}

	if err := WriteBatchResults(filename, out); err != nil {
		return fmt.Errorf("writing results file: %w", err)
	}
	if summary := FormatJudgeSummary(out); summary != "" {
		fmt.Printf("\n%s", summary)
	}
	fmt.Printf("\nResults saved to %s\n", filename)
	return nil
}

// WriteBatchResults writes a results file atomically: to a temporary file in the same
// directory, then renamed over the target. Rewriting the answers-only file with the
// verdicts therefore never leaves a half-written file behind.
func WriteBatchResults(filename string, out *BatchOutput) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling results: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), "."+filepath.Base(filename)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filename)
}
