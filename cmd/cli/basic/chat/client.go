package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// FindModelName asks the inference server for its models and returns the first
// one. A server that cannot list models (AWS Bedrock, or chat.api=anthropic)
// yields an error wrapping ErrModelListingUnsupported that tells the operator to
// set chat.model; an unreachable server or one returning no models is an error.
func FindModelName(backend BackendConfig) (string, error) {
	b, err := NewBackend(backend)
	if err != nil {
		return "", err
	}
	models, err := b.ListModels(context.Background())
	if errors.Is(err, ErrModelListingUnsupported) {
		return "", ModelRequiredError()
	} else if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("server returned no models")
	}
	return models[0], nil
}

// Client runs the interactive chat REPL against the inference server addressed by
// backend, with RAG retrieval when a knowledge client is given.
func Client(backend BackendConfig, knowledgeClient *knowledge.OpenSearchClient, kapaClient *knowledge.KapaClient, embeddingModelID string, llmModelName string, prompts PromptConfig, temperature float64, verbose bool) error {
	fmt.Printf("Using inference server at %v\n", backend.BaseURL)

	llm, err := NewBackend(backend)
	if err != nil {
		return err
	}

	// Check if server is reachable
	if err := handshake(backend.BaseURL); err != nil {
		return err
	}

	defaultKnowledgeBase, _ := knowledge.KnowledgeBaseNameFromIndex(knowledge.DefaultIndexName())

	if knowledgeClient != nil {
		fmt.Printf(
			"Using the `%s` knowledge base at %v\n\t> Use `%s` to see other available knowledge bases\n",
			defaultKnowledgeBase,
			knowledgeClient.URL(),
			cmdUseKnowledge,
		)
	}
	if kapaClient != nil {
		fmt.Printf("\t> Use `%s` to select Kapa.ai source groups\n", cmdUseKapa)
	}
	fmt.Println()

	if llmModelName == "" {
		llmModelName, err = findModelName(llm, verbose)
		if err != nil {
			return err
		}
	}
	if verbose {
		fmt.Printf("Using model %v\n", llmModelName)
		if notice := temperatureNotice(backend.API, temperature); notice != "" {
			fmt.Println(notice)
		}
	}

	if err := checkServer(llm, llmModelName); err != nil {
		return err
	}

	fmt.Println("Type your prompt, then ENTER to submit. CTRL-C to quit.")

	// Build autocomplete for slash commands.
	var completions []readline.PrefixCompleterInterface
	for _, cmd := range slashCommands {
		completions = append(completions, readline.PcItem(cmd.name))
	}

	rlConfig := &readline.Config{
		Prompt:                 color.RedString("» "),
		AutoComplete:           readline.NewPrefixCompleter(completions...),
		Listener:               slashHinter(),
		Painter:                syntaxPainter{},
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

	// The configured prompt is sent unconditionally — retrieval availability
	// never swaps in a hidden substitute, so what `prompt init` shows is what
	// runs.
	conv := &conversation{
		model:       llmModelName,
		system:      prompts.ChatSystemPrompt,
		temperature: &temperature,
	}

	session := &Session{
		KnowledgeClient:  knowledgeClient,
		KapaClient:       kapaClient,
		EmbeddingModelID: embeddingModelID,
		ActiveIndexes:    []string{knowledge.DefaultIndexName()},
	}

	// Saved-chat history is stored client-locally in daemonless mode. chatID pins
	// the record this session saves to, so a second /save updates it in place.
	chatStore, _ := localChatStore()
	var chatID string

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

		// Handle slash commands without sending to the LLM. Readline is torn down
		// and recreated around them because /use-knowledge and /history drive the
		// terminal via huh, which conflicts with an active readline.
		if strings.HasPrefix(prompt, "/") {
			rl.Close()
			verb, args, _ := strings.Cut(strings.TrimSpace(prompt), " ")
			switch verb {
			case cmdSave:
				if id, ok := saveDirectChat(chatStore, chatID, args, llmModelName, session, conv.history); ok {
					chatID = id
				}
			case cmdHistory:
				if msgs, id, ok := resumeDirectChat(chatStore, session); ok {
					conv.history = msgs
					chatID = id
				}
			default:
				handleSlashCommand(prompt, session)
			}
			rl, err = readline.NewEx(rlConfig)
			if err != nil {
				return fmt.Errorf("error reinitializing readline: %w", err)
			}
			log.SetOutput(rl.Stderr())
			continue
		}

		if len(prompt) > 0 {
			rl.SaveHistory(prompt)
			if err := handlePrompt(llm, conv, prompt, session, verbose); err != nil {
				return err
			}
		}
	}
	fmt.Println("Closing chat")

	return nil
}

func handshake(baseURL string) error {
	stopProgress := common.StartProgressSpinner("Connecting to server")
	defer stopProgress()

	parsedURL, err := url.Parse(baseURL)
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

func checkServer(backend Backend, modelName string) error {
	stopProgress := common.StartProgressSpinner("Waiting for server to be ready")
	defer stopProgress()

	const (
		retryInterval = 5 * time.Second
		waitTimeout   = 60 * time.Second
	)
	start := time.Now()
	for {
		err := backend.Ping(context.Background(), modelName)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrServerLoading) {
			if time.Since(start) > waitTimeout {
				// Stop waiting
				return fmt.Errorf("no models available on server\n\n%s\n%s",
					common.SuggestServerStartup(),
					common.SuggestServerLogs())
			}
			time.Sleep(retryInterval)
			continue
		}
		var statusErr *StatusError
		if errors.As(err, &statusErr) {
			if statusErr.Unauthorized() {
				return fmt.Errorf("api: %s\n\nSet the CHAT_API_KEY environment variable to authenticate", statusErr.Error())
			}
			return fmt.Errorf("api: %s", statusErr.Error())
		}
		return fmt.Errorf("%s\n\n%s", err,
			common.SuggestServerLogs())
	}
}

func findModelName(backend Backend, _ bool) (string, error) {
	stopProgress := common.StartProgressSpinner("Looking up model name")
	defer stopProgress()

	const (
		retryInterval = 5 * time.Second
		waitTimeout   = 10 * time.Second
	)
	start := time.Now()
	for {
		models, err := backend.ListModels(context.Background())
		if errors.Is(err, ErrModelListingUnsupported) {
			return "", ModelRequiredError()
		} else if err != nil {
			return "", err
		}

		if len(models) == 0 {
			// This can happen when OpenVINO Model Server is starting up
			if time.Since(start) > waitTimeout {
				// Stop waiting
				return "", fmt.Errorf("server returned no models\n\n%s\n%s",
					common.SuggestServerStartup(),
					common.SuggestServerLogs())
			}
			time.Sleep(retryInterval)
			continue
		} else if len(models) > 1 {
			return "", fmt.Errorf("expected one but server returned multiple models: %s", strings.Join(models, ", "))
		}

		return models[0], nil
	} // end for
}

func handlePrompt(backend Backend, conv *conversation, prompt string, session *Session, verbose bool) error {
	// RAG augmentation applies only when a knowledge client is present AND at
	// least one base is active. With no active base the prompt is answered
	// without retrieval (mirroring the daemon's LiveSession.Prompt), so a plain
	// greeting like "Hi" gets a natural reply instead of a grounded refusal.
	hasRAG := session.KnowledgeClient != nil && len(session.ActiveIndexes) > 0
	hasKapa := session.KapaClient != nil && len(session.ActiveKapaGroups) > 0
	hasContext := hasRAG || hasKapa

	// Rewrite the query for richer BM25 matching using conversation context.
	// On the first turn (no history) this returns the original prompt.
	lexicalQuery := prompt
	ragContext := ""
	if hasContext {
		lexicalQuery = rewriteSearchQuery(backend, conv.model, conv.history, prompt, verbose)
		// Retrieve RAG context from knowledge base (no-op when unavailable).
		ragContext = retrieveContext(session, prompt, lexicalQuery, verbose)
	}

	// Build the message sent to the LLM: augmented when context is found.
	// When a base is active but retrieval returned nothing, inject an explicit
	// empty-context note so the grounding rules in the system prompt apply and
	// the model does not answer from parametric knowledge.
	llmPrompt := prompt
	if ragContext != "" {
		llmPrompt = buildRAGPrompt(ragContext, "", "", prompt)
	} else if hasContext {
		llmPrompt = buildRAGPrompt("No relevant context was retrieved for this query.", "", "", prompt)
	}

	// The augmented prompt is sent to the API, but only the original prompt is
	// kept in history.
	req := conv.turnRequest(llmPrompt)

	if verbose {
		reqDebugString, _ := json.Marshal(req)
		fmt.Printf("Sending request: %s\n", reqDebugString)
	}

	stopProgress := sync.OnceFunc(common.StartProgressSpinner("Generating an answer"))
	reply, err := backend.Stream(context.Background(), req, printToken(stopProgress))
	stopProgress()
	if err != nil {
		// A refusal or an exhausted output budget fails this turn only: report
		// it and keep the session, without recording the turn.
		var refusal *RefusalError
		var ceiling *OutputCeilingError
		if errors.As(err, &refusal) || errors.As(err, &ceiling) {
			fmt.Printf("\n%s\n", color.YellowString(err.Error()))
			return nil
		}
		return streamError(err)
	}

	conv.record(prompt, reply)
	fmt.Println()

	return nil
}

// printToken renders streamed output: reasoning in blue, answer text plain. The
// spinner is stopped by the first token so it never interleaves with output. A
// line break separates a reasoning block from the answer that follows it.
func printToken(stopProgress func()) StreamFunc {
	var last TokenKind
	var lastText string
	return func(kind TokenKind, content string) error {
		stopProgress()
		if last == TokenThink && kind == TokenAnswer && !strings.HasSuffix(lastText, "\n") {
			fmt.Println()
		}
		if kind == TokenThink {
			fmt.Printf("%s", color.BlueString(content))
		} else {
			fmt.Printf("%s", content)
		}
		last, lastText = kind, content
		return nil
	}
}

// streamError adds troubleshooting guidance to a failed streaming turn.
func streamError(err error) error {
	if errors.Is(err, syscall.ECONNREFUSED) { // connection refused before streaming
		return fmt.Errorf("connection refused\n\n%s",
			common.SuggestServerLogs())
	} else if errors.Is(err, io.ErrUnexpectedEOF) {
		fmt.Println() // break the line after incomplete stream
		return fmt.Errorf("connection closed by server\n\n%s",
			common.SuggestServerLogs())
	}
	return fmt.Errorf("%s\n\n%s", err,
		common.SuggestServerLogs())
}

func filterInput(r rune) (rune, bool) {
	switch r {
	// block CtrlZ feature
	case readline.CharCtrlZ:
		return r, false
	}
	return r, true
}
