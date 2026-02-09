package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

// FindModelName queries the OpenAI-compatible API for available models
// and returns the first model name. Returns an error if the server is
// unreachable or returns no models.
func FindModelName(baseUrl string) (string, error) {
	modelService := openai.NewModelService(option.WithBaseURL(baseUrl))
	modelPage, err := modelService.List(context.Background())
	if err != nil {
		return "", err
	}
	if len(modelPage.Data) == 0 {
		return "", fmt.Errorf("server returned no models")
	}
	return modelPage.Data[0].ID, nil
}

func Client(baseUrl string, knowledgeClient *knowledge.OpenSearchClient, embeddingModelID string, modelName string, verbose bool) error {
	fmt.Printf("Using inference server at %v\n", baseUrl)

	// Check if server is reachable
	if err := handshake(baseUrl); err != nil {
		return err
	}

	defaultKnowledgeBase, _ := knowledge.KnowledgeBaseNameFromIndex(knowledge.DefaultIndexName())

	if knowledgeClient != nil {
		fmt.Printf(
			"Using the `%s` knowledge base at %v\n\t> Use /active-context to see other available knowledge bases\n\n",
			defaultKnowledgeBase,
			knowledgeClient.URL())
	}

	if modelName == "" {
		var err error
		modelName, err = findModelName(baseUrl, verbose)
		if err != nil {
			return err
		}
	}
	if verbose {
		fmt.Printf("Using model %v\n", modelName)
	}

	// OpenAI API Client
	client := openai.NewClient(option.WithBaseURL(baseUrl))

	if err := checkServer(client, modelName); err != nil {
		return err
	}

	fmt.Println("Type your prompt, then ENTER to submit. CTRL-C to quit.")

	// Build autocomplete for slash commands.
	var completions []readline.PrefixCompleterInterface
	for _, cmd := range slashCommands {
		completions = append(completions, readline.PcItem(cmd))
	}

	rlConfig := &readline.Config{
		Prompt:                 color.RedString("» "),
		AutoComplete:           readline.NewPrefixCompleter(completions...),
		Listener:               slashHinter(),
		DisableAutoSaveHistory: true,
		InterruptPrompt:        "^C",

		HistorySearchFold:   true,
		FuncFilterInputRune: filterInput,
	}

	rl, err := readline.NewEx(rlConfig)
	if err != nil {
		return fmt.Errorf("error initializing readline: %w", err)
	}
	defer func() { rl.Close() }()
	//rl.CaptureExitSignal() // Should readline capture and handle the exit signal? - Can be used to interrupt the chat response stream.
	log.SetOutput(rl.Stderr())

	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are a helpful assistant."),
		},
		Model: modelName,
	}

	session := &Session{
		KnowledgeClient:  knowledgeClient,
		EmbeddingModelID: embeddingModelID,
		ActiveIndexes:    []string{knowledge.DefaultIndexName()},
	}

	for {
		prompt, err := rl.Readline()
		clearSlashHints()
		if errors.Is(err, readline.ErrInterrupt) {
			if len(prompt) == 0 {
				break
			} else {
				continue
			}
		} else if err == io.EOF {
			break
		}
		if prompt == "exit" {
			break
		}

		// Handle slash commands (e.g. /active-context) without sending to the LLM.
		if strings.HasPrefix(prompt, "/") {
			rl.Close()
			handleSlashCommand(prompt, session)
			rl, err = readline.NewEx(rlConfig)
			if err != nil {
				return fmt.Errorf("error reinitializing readline: %w", err)
			}
			log.SetOutput(rl.Stderr())
			continue
		}

		if len(prompt) > 0 {
			rl.SaveHistory(prompt)
			params, err = handlePrompt(client, params, prompt, session, verbose)
			if err != nil {
				return err
			}
		}
	}
	fmt.Println("Closing chat")

	return nil
}

func handshake(baseUrl string) error {
	stopProgress := common.StartProgressSpinner("Connecting to server")
	defer stopProgress()

	parsedURL, err := url.Parse(baseUrl)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		if parsedURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("connection refused\n\n%s\n%s",
			common.SuggestServerStartup(),
			common.SuggestServerLogs())
	} else if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func checkServer(client openai.Client, modelName string) error {
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("Are you up?"),
		},
		Model:               modelName,
		MaxCompletionTokens: openai.Int(1),
		MaxTokens:           openai.Int(1), // for runtimes that don't yet support MaxCompletionTokens
	}

	stopProgress := common.StartProgressSpinner("Waiting for server to be ready")
	defer stopProgress()

	const (
		retryInterval = 5 * time.Second
		waitTimeout   = 60 * time.Second
	)
	start := time.Now()
	for {
		_, err := client.Chat.Completions.New(context.Background(), params)
		if err != nil {
			var apiError *openai.Error
			if errors.As(err, &apiError) {
				// llama-server starting up
				// Error: POST "http://localhost:8328/v1/chat/completions": 503 Service Unavailable {"message":"Loading model","type":"unavailable_error","code":503}
				if apiError.StatusCode == http.StatusServiceUnavailable && apiError.Type == "unavailable_error" {
					if time.Since(start) > waitTimeout {
						// Stop waiting
						return fmt.Errorf("no models available on server\n\n%s\n%s",
							common.SuggestServerStartup(),
							common.SuggestServerLogs())
					}
					time.Sleep(retryInterval)
					continue
				}
				return fmt.Errorf("api: %s", apiError.Error())
			} else {
				return fmt.Errorf("%s\n\n%s", err,
					common.SuggestServerLogs())
			}
		}

		return nil
	}
}

func findModelName(baseUrl string, verbose bool) (string, error) {
	stopProgress := common.StartProgressSpinner("Looking up model name")
	defer stopProgress()

	modelService := openai.NewModelService(option.WithBaseURL(baseUrl))

	const (
		retryInterval = 5 * time.Second
		waitTimeout   = 10 * time.Second
	)
	start := time.Now()
	for {
		modelPage, err := modelService.List(context.Background())
		if err != nil {
			return "", err
		}

		if len(modelPage.Data) == 0 {
			// This can happen when OpenVINO Model Server is starting up
			if time.Since(start) > waitTimeout {
				// Stop waiting
				return "", fmt.Errorf("server returned no models\n\n%s\n%s",
					common.SuggestServerStartup(),
					common.SuggestServerLogs())
			}
			time.Sleep(retryInterval)
			continue
		} else if len(modelPage.Data) > 1 {
			var names []string
			for _, model := range modelPage.Data {
				names = append(names, model.ID)
			}
			return "", fmt.Errorf("expected one but server returned multiple models: %s", strings.Join(names, ", "))
		}

		return modelPage.Data[0].ID, nil
	} // end for
}

func handlePrompt(client openai.Client, params openai.ChatCompletionNewParams, prompt string, session *Session, verbose bool) (openai.ChatCompletionNewParams, error) {
	// Rewrite the query for richer BM25 matching using conversation context.
	// On the first turn (no history) this returns the original prompt.
	lexicalQuery := prompt
	if session.KnowledgeClient != nil {
		lexicalQuery = rewriteSearchQuery(client, params.Model, params.Messages, prompt, verbose)
	}

	// Retrieve RAG context from knowledge base (no-op when unavailable).
	ragContext := retrieveContext(session, prompt, lexicalQuery, verbose)

	// Build the message sent to the LLM: augmented when context is found,
	// plain otherwise.
	llmPrompt := prompt
	if ragContext != "" {
		llmPrompt = buildRAGPrompt(ragContext, prompt)
	}

	// Build a temporary copy of the message history so the augmented prompt
	// is sent to the API but only the original prompt is kept in history.
	apiMessages := make([]openai.ChatCompletionMessageParamUnion, len(params.Messages))
	copy(apiMessages, params.Messages)
	apiMessages = append(apiMessages, openai.UserMessage(llmPrompt))

	apiParams := params
	apiParams.Messages = apiMessages

	if verbose {
		paramDebugString, _ := json.Marshal(apiParams)
		fmt.Printf("Sending request: %s\n", paramDebugString)
	}

	stopProgress := common.StartProgressSpinner("Generating an answer")
	stream := client.Chat.Completions.NewStreaming(context.Background(), apiParams)
	stopProgress()
	
	appendParam, err := processStream(stream)
	if err != nil {
		return params, err
	}

	// Store the original prompt (not the augmented one) plus the assistant
	// response in the conversation history.
	params.Messages = append(params.Messages, openai.UserMessage(prompt))
	if appendParam != nil {
		params.Messages = append(params.Messages, *appendParam)
	}
	fmt.Println()

	return params, nil
}

func processStream(stream *ssestream.Stream[openai.ChatCompletionChunk]) (*openai.ChatCompletionMessageParamUnion, error) {
	// optionally, an accumulator helper can be used
	acc := openai.ChatCompletionAccumulator{}

	// An opening <think> tag will change the output color to indicate reasoning.
	thinking := false

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if _, ok := acc.JustFinishedContent(); ok {
			//fmt.Println("\nContent stream finished")
		}

		// if using tool calls
		if tool, ok := acc.JustFinishedToolCall(); ok {
			fmt.Printf("Tool call stream finished %d: %s %s", tool.Index, tool.Name, tool.Arguments)
		}

		if refusal, ok := acc.JustFinishedRefusal(); ok {
			fmt.Printf("Refusal stream finished: %s", refusal)
		}

		// Print chunks as they are received
		if len(chunk.Choices) > 0 {
			lastChunk := chunk.Choices[0].Delta.Content

			if strings.Contains(lastChunk, "<think>") {
				thinking = true
				fmt.Printf("%s", color.BlueString(lastChunk))
			} else if strings.Contains(lastChunk, "</think>") {
				thinking = false
				fmt.Printf("%s", color.BlueString(lastChunk))

			} else if thinking {
				fmt.Printf("%s", color.BlueString(lastChunk))

			} else {
				fmt.Printf("%s", lastChunk)
			}
		}
	}

	if err := stream.Err(); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) { // connection refused before streaming
			return nil, fmt.Errorf("connection refused\n\n%s",
				common.SuggestServerLogs())
		} else if errors.Is(err, io.ErrUnexpectedEOF) {
			fmt.Println() // break the line after incomplete stream
			return nil, fmt.Errorf("connection closed by server\n\n%s",
				common.SuggestServerLogs())
		}
		return nil, fmt.Errorf("%s\n\n%s", err,
			common.SuggestServerLogs())
	}

	// After the stream is finished, acc can be used like a ChatCompletion
	appendParam := acc.Choices[0].Message.ToParam()
	if acc.Choices[0].Message.Content == "" {
		return nil, nil
	}
	return &appendParam, nil
}

func filterInput(r rune) (rune, bool) {
	switch r {
	// block CtrlZ feature
	case readline.CharCtrlZ:
		return r, false
	}
	return r, true
}
