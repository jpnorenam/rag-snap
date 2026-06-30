package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/charmbracelet/huh"
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

	// Track the active bases locally so the /use-knowledge menu can pre-select
	// the current set; kept in sync with the daemon's acknowledged set.
	activeBases := append([]string{}, bases...)

	// Build autocomplete for slash commands, matching the direct REPL.
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
		clearSlashHints()
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
		// holds the active set for the session. With no inline args it opens
		// the same interactive multi-select menu as the direct REPL. Readline
		// is torn down and recreated around the menu because huh and readline
		// both drive the terminal and conflict if left active together.
		if verb, _, _ := strings.Cut(strings.TrimSpace(prompt), " "); verb == cmdUseKnowledge {
			rl.Close()
			acked, uerr := remoteSetActiveBases(ctx, dc, session, prompt, activeBases)
			if uerr != nil {
				fmt.Printf("Error: %v\n", uerr)
			} else {
				activeBases = acked
			}
			rl, err = readline.NewEx(rlConfig)
			if err != nil {
				return fmt.Errorf("error reinitializing readline: %w", err)
			}
			log.SetOutput(rl.Stderr())
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

// remoteSetActiveBases resolves the desired active knowledge bases and sends
// them to the daemon as a set-active-kbs frame, returning the acknowledged set.
// "/use-knowledge base1 base2 ..." uses the inline names; bare "/use-knowledge"
// opens the same interactive multi-select menu as the direct REPL, fetching the
// available bases from the daemon over the socket. The daemon expects base names
// (not full index names) and applies the index prefix itself.
func remoteSetActiveBases(ctx context.Context, dc *apiclient.Client, session *apiclient.ChatSession, input string, current []string) ([]string, error) {
	_, args, _ := strings.Cut(strings.TrimSpace(input), " ")

	var bases []string
	if strings.TrimSpace(args) != "" {
		bases = strings.Fields(args)
	} else {
		selected, ok, err := remoteSelectBasesMenu(ctx, dc, current)
		if err != nil {
			return current, err
		}
		if !ok {
			// User cancelled the menu — leave the active set unchanged.
			return current, nil
		}
		bases = selected
	}

	if err := session.SetActiveBases(ctx, bases); err != nil {
		return current, err
	}
	msg, err := session.Read(ctx)
	if err != nil {
		return current, err
	}
	if msg.Type == "error" {
		return current, fmt.Errorf("%s", msg.Error)
	}
	if len(msg.Bases) == 0 {
		fmt.Println("Active knowledge bases: (none)")
	} else {
		fmt.Printf("Active knowledge bases: %s\n", strings.Join(msg.Bases, ", "))
	}
	return msg.Bases, nil
}

// remoteSelectBasesMenu lists knowledge bases from the daemon and presents the
// interactive multi-select menu, pre-selecting the currently active set. The
// boolean is false when the user cancelled (Ctrl+C / Esc).
func remoteSelectBasesMenu(ctx context.Context, dc *apiclient.Client, current []string) ([]string, bool, error) {
	stop := common.StartProgressSpinner("Fetching knowledge bases")
	bases, err := dc.ListKnowledge(ctx)
	stop()
	if err != nil {
		return nil, false, fmt.Errorf("listing knowledge bases: %w", err)
	}
	if len(bases) == 0 {
		fmt.Println("No knowledge bases found. Create one with 'knowledge create <name>'.")
		return nil, false, nil
	}

	options := make([]huh.Option[string], len(bases))
	for i, kb := range bases {
		label := fmt.Sprintf("%s (%s docs, %s)", kb.Name, kb.DocsCount, kb.StoreSize)
		options[i] = huh.NewOption(label, kb.Name)
	}

	selected := append([]string{}, current...)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select active knowledge bases").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		// User cancelled (Ctrl+C / Esc) — keep existing context.
		return nil, false, nil
	}
	return selected, true, nil
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
