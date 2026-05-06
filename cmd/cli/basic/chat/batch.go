package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/openai/openai-go/v3"
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

// mergeKeywords places manifest keywords first (higher priority), then appends
// generated keywords that are not already present, deduplicating case-insensitively.
// generated is a space-separated keyword string from rewriteSearchQuery.
// manifestKWs are the optional keywords from the batch manifest.
// Returns a space-separated string ready for use as a lexical search query.
func mergeKeywords(generated string, manifestKWs []string) string {
	if len(manifestKWs) == 0 {
		return generated
	}

	// Manifest keywords lead; build seen set from them first.
	genFields := strings.Fields(generated)
	seen := make(map[string]struct{}, len(manifestKWs)+len(genFields))
	merged := make([]string, 0, len(manifestKWs)+len(genFields))
	for _, kw := range manifestKWs {
		lower := strings.ToLower(kw)
		if _, exists := seen[lower]; !exists {
			seen[lower] = struct{}{}
			merged = append(merged, kw)
		}
	}

	// Append generated keywords not already covered by manifest keywords.
	for _, kw := range genFields {
		lower := strings.ToLower(kw)
		if _, exists := seen[lower]; !exists {
			seen[lower] = struct{}{}
			merged = append(merged, kw)
		}
	}

	return strings.Join(merged, " ")
}

// BatchQuestion describes a single Q&A task within a batch manifest.
type BatchQuestion struct {
	ID       string      `yaml:"id,omitempty"`
	Question string      `yaml:"question"`
	Keywords KeywordList `yaml:"keywords,omitempty"`
}

// BatchManifest is the top-level structure of a batch chat YAML file.
type BatchManifest struct {
	Version        string          `yaml:"version"`
	Model          string          `yaml:"model,omitempty"`
	KnowledgeBases []string        `yaml:"knowledge_bases,omitempty"`
	Prompt         string          `yaml:"prompt,omitempty"`
	Questions      []BatchQuestion `yaml:"questions"`
}

// BatchResult holds the answer for a single question.
type BatchResult struct {
	ID       string `json:"id,omitempty"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type batchOutput struct {
	GeneratedAt string        `json:"generated_at"`
	Model       string        `json:"model"`
	Results     []BatchResult `json:"results"`
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
	return &manifest, nil
}

// ProcessBatchChat runs each question in the manifest through the RAG+LLM pipeline,
// prints Q&A pairs to the terminal, and writes all results to a timestamped JSON file.
func ProcessBatchChat(
	baseURL string,
	knowledgeClient *knowledge.OpenSearchClient,
	embeddingModelID string,
	manifest *BatchManifest,
	prompts PromptConfig,
	verbose bool,
) error {
	client := openai.NewClient(clientOptions(baseURL)...)

	modelName := manifest.Model
	if modelName == "" {
		var err error
		modelName, err = findModelName(baseURL, verbose)
		if err != nil {
			return fmt.Errorf("resolving model name: %w", err)
		}
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
		EmbeddingModelID: embeddingModelID,
		ActiveIndexes:    activeIndexes,
	}

	fmt.Printf("Found %d questions in batch manifest version %s\n", len(manifest.Questions), manifest.Version)

	defaultSystemPrompt := prompts.AnswerSystemPrompt
	if manifest.Prompt != "" {
		// Append the non-negotiable source rules so custom prompts cannot
		// accidentally bypass [CANONICAL]/[UPSTREAM] grounding behaviour.
		defaultSystemPrompt = manifest.Prompt + "\n\n" + prompts.SourceRules
	}

	ctx := context.Background()
	results := make([]BatchResult, 0, len(manifest.Questions))

	for i, q := range manifest.Questions {
		fmt.Printf("[%d/%d] Question: %s\n", i+1, len(manifest.Questions), q.Question)

		// nil history: each question is extracted in isolation, with no prior conversation context.
		lexicalQuery := rewriteSearchQuery(client, modelName, nil, q.Question, verbose)
		// Manifest keywords lead in the lexical query (higher BM25 priority).
		lexicalQuery = mergeKeywords(lexicalQuery, q.Keywords)

		// Use the full merged lexical query (manifest + generated keywords) to
		// steer the vector search as well. This ensures that user-provided keywords
		// like "magnum" influence embedding similarity, not just BM25 scoring.
		semanticQuery := q.Question
		if lexicalQuery != q.Question {
			semanticQuery = q.Question + " " + lexicalQuery
		}
		ragContext := retrieveContext(session, semanticQuery, lexicalQuery, verbose)

		// When no context was retrieved there is nothing to ground the answer on.
		// Skip the LLM call entirely and emit the fixed no-answer string to avoid
		// the model hallucinating from parametric knowledge.
		if ragContext == "" {
			const noContext = "The provided context does not contain enough information to answer this question."
			fmt.Printf("Answer: %s\n---\n", noContext)
			results = append(results, BatchResult{
				ID:       q.ID,
				Question: q.Question,
				Answer:   noContext,
			})
			continue
		}

		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(defaultSystemPrompt),
				openai.UserMessage(buildRAGPrompt(ragContext, q.Question)),
			},
			Model: modelName,
		})
		if err != nil {
			fmt.Printf("error on question %d: %v\n", i+1, err)
			continue
		}

		var answer string
		if len(resp.Choices) > 0 {
			answer = StripThinkTags(resp.Choices[0].Message.Content)
		}

		fmt.Printf("Answer: %s\n---\n", answer)

		results = append(results, BatchResult{
			ID:       q.ID,
			Question: q.Question,
			Answer:   answer,
		})
	}

	if len(results) == 0 {
		return fmt.Errorf("all questions failed; no results to write")
	}

	now := time.Now()
	filename := fmt.Sprintf("batch-results-%s.json", now.Format("20060102-150405"))
	out := batchOutput{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Model:       modelName,
		Results:     results,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling results: %w", err)
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("writing results file: %w", err)
	}

	fmt.Printf("\nResults saved to %s\n", filename)
	return nil
}
