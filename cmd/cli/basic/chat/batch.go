package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/openai/openai-go/v3"
	"gopkg.in/yaml.v3"
)

// BatchQuestion describes a single Q&A task within a batch manifest.
type BatchQuestion struct {
	ID       string `yaml:"id,omitempty"`
	Question string `yaml:"question"`
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

	defaultSystemPrompt := ragAnswerSystemPrompt
	if manifest.Prompt != "" {
		defaultSystemPrompt = manifest.Prompt
	}

	ctx := context.Background()
	results := make([]BatchResult, 0, len(manifest.Questions))

	for i, q := range manifest.Questions {
		fmt.Printf("[%d/%d] Question: %s\n", i+1, len(manifest.Questions), q.Question)

		// nil history: each question is extracted in isolation, with no prior conversation context.
		lexicalQuery := rewriteSearchQuery(client, modelName, nil, q.Question, verbose)
		ragContext := retrieveContext(session, q.Question, lexicalQuery, verbose)

		systemPrompt := "You are a helpful assistant."
		llmPrompt := q.Question
		if ragContext != "" {
			systemPrompt = defaultSystemPrompt
			llmPrompt = buildRAGPrompt(ragContext, q.Question)
		}

		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemPrompt),
				openai.UserMessage(llmPrompt),
			},
			Model: modelName,
		})
		if err != nil {
			fmt.Printf("error on question %d: %v\n", i+1, err)
			continue
		}

		var answer string
		if len(resp.Choices) > 0 {
			answer = stripThinkTags(resp.Choices[0].Message.Content)
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
