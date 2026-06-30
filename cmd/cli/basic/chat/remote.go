package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
)

// RemoteClient runs the interactive chat REPL against a ragd daemon over its
// unix socket, instead of talking to the inference/knowledge backends directly.
// The daemon owns the session state (history, active bases, resolved model); the
// REPL only sends prompts and renders streamed token/think frames. /use-knowledge
// becomes a set-active-kbs control message; other slash commands behave as in the
// direct REPL where they make sense.
func RemoteClient(dc *apiclient.Client, llmModelName string, bases []string, temperature float64) error {
	ctx := context.Background()

	stop := common.StartProgressSpinner("Connecting to ragd")
	session, err := dc.StartChat(ctx, llmModelName, bases, temperature)
	stop()
	if err != nil {
		return fmt.Errorf("starting chat session: %w", err)
	}
	defer session.Close()

	if session.Model != "" {
		fmt.Printf("Using model %v (via ragd)\n", session.Model)
	}
	fmt.Println("Type your prompt, then ENTER to submit. CTRL-C to quit.")

	rlConfig := &readline.Config{
		Prompt:                 color.RedString("» "),
		DisableAutoSaveHistory: true,
		InterruptPrompt:        "^C",
		HistorySearchFold:      true,
		FuncFilterInputRune:    filterInput,
	}
	rl, err := readline.NewEx(rlConfig)
	if err != nil {
		return fmt.Errorf("error initializing readline: %w", err)
	}
	defer func() { rl.Close() }()
	log.SetOutput(rl.Stderr())

	for {
		prompt, err := rl.Readline()
		if errors.Is(err, readline.ErrInterrupt) {
			if len(prompt) == 0 {
				break
			}
			continue
		} else if err == io.EOF {
			break
		}
		if prompt == "exit" {
			break
		}

		// /use-knowledge maps to a set-active-kbs control frame; the daemon
		// holds the active set for the session.
		if strings.HasPrefix(prompt, cmdUseKnowledge) {
			if err := remoteSetActiveBases(ctx, session, prompt); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
			continue
		}
		if strings.HasPrefix(prompt, "/") {
			fmt.Printf("Command %q is not available over the daemon; use it in direct mode.\n", prompt)
			continue
		}

		if len(prompt) == 0 {
			continue
		}
		rl.SaveHistory(prompt)
		if err := remotePromptTurn(ctx, session, prompt); err != nil {
			return err
		}
	}
	fmt.Println("Closing chat")
	return nil
}

// remoteSetActiveBases parses "/use-knowledge base1 base2 ..." and sends the
// active-KB set to the daemon, printing the acknowledged set.
func remoteSetActiveBases(ctx context.Context, session *apiclient.ChatSession, input string) error {
	_, args, _ := strings.Cut(strings.TrimSpace(input), " ")
	var bases []string
	for _, b := range strings.Fields(args) {
		bases = append(bases, b)
	}
	if err := session.SetActiveBases(ctx, bases); err != nil {
		return err
	}
	msg, err := session.Read(ctx)
	if err != nil {
		return err
	}
	if msg.Type == "error" {
		return fmt.Errorf("%s", msg.Error)
	}
	fmt.Printf("Active knowledge bases: %s\n", strings.Join(msg.Bases, ", "))
	return nil
}

// remotePromptTurn sends one prompt and renders streamed frames until the
// terminal "done" frame, colouring <think> content like the direct REPL.
func remotePromptTurn(ctx context.Context, session *apiclient.ChatSession, prompt string) error {
	if err := session.Prompt(ctx, prompt); err != nil {
		return err
	}
	for {
		msg, err := session.Read(ctx)
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}
		switch msg.Type {
		case string(TokenAnswer):
			fmt.Print(msg.Content)
		case string(TokenThink):
			fmt.Print(color.BlueString(msg.Content))
		case "done":
			fmt.Println()
			return nil
		case "error":
			fmt.Println()
			return fmt.Errorf("%s", msg.Error)
		}
	}
}
