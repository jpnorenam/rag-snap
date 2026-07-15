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
	// A closure (not defer session.Close()) so a session swapped in by /history is
	// the one closed at exit, and the original was already closed at the swap.
	defer func() { session.Close() }()

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
		// /search runs retrieval-only over the daemon: the daemon owns the
		// embedding model and OpenSearch client, so the REPL just forwards the
		// query plus the locally-tracked active bases and renders the hits.
		if verb, args, _ := strings.Cut(strings.TrimSpace(prompt), " "); verb == cmdSearch {
			remoteSearch(ctx, dc, args, activeBases)
			continue
		}
		// /save persists the daemon-owned session to the shared chat store.
		if verb, args, _ := strings.Cut(strings.TrimSpace(prompt), " "); verb == cmdSave {
			remoteSave(ctx, session, args)
			continue
		}
		// /history lists the shared store and, on selection, closes this session
		// and starts a fresh one resumed from the chosen chat. Readline is torn
		// down around the huh picker, as for /use-knowledge.
		if verb, _, _ := strings.Cut(strings.TrimSpace(prompt), " "); verb == cmdHistory {
			rl.Close()
			newSession, newBases, ok := remoteHistory(ctx, dc)
			if ok {
				session.Close()
				session = newSession
				activeBases = newBases
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

// remoteSave sends a save control frame and prints the daemon's acknowledgement.
// The daemon owns the transcript, so the REPL only forwards the optional title.
func remoteSave(ctx context.Context, session *apiclient.ChatSession, args string) {
	if err := session.Save(ctx, strings.TrimSpace(args)); err != nil {
		fmt.Printf("Could not save chat: %v\n", err)
		return
	}
	msg, err := session.Read(ctx)
	if err != nil {
		fmt.Printf("Could not save chat: %v\n", err)
		return
	}
	switch msg.Type {
	case "saved":
		fmt.Printf("Saved chat as %q.\n", msg.Title)
	case "error":
		fmt.Println(msg.Error)
	default:
		fmt.Printf("Unexpected response while saving: %s\n", msg.Type)
	}
}

// remoteHistory lists the shared chat store, lets the user pick a chat, and
// resumes it as a new session. It returns the new session and its restored active
// bases; ok is false when the user cancelled or nothing could be resumed. On
// success the caller closes the previous session.
func remoteHistory(ctx context.Context, dc *apiclient.Client) (*apiclient.ChatSession, []string, bool) {
	stop := common.StartProgressSpinner("Fetching saved chats")
	summaries, err := dc.ListChats(ctx, "")
	stop()
	if err != nil {
		fmt.Printf("Saved chats are not available over this ragd (it may be an older version): %v\n", err)
		return nil, nil, false
	}

	picked, ok := pickSavedChat(summaries)
	if !ok {
		return nil, nil, false
	}

	stop = common.StartProgressSpinner("Resuming chat")
	session, err := dc.ResumeChat(ctx, picked.ID)
	stop()
	if err != nil {
		fmt.Printf("Could not resume chat: %v\n", err)
		return nil, nil, false
	}

	var bases []string
	if session.Restored != nil {
		renderTranscript(session.Restored.Turns)
		bases = session.Restored.Bases
		if len(session.Restored.DroppedBases) > 0 {
			fmt.Printf("Note: skipping knowledge base(s) that no longer exist: %s\n", strings.Join(session.Restored.DroppedBases, ", "))
		}
		fmt.Printf("Resumed %q. Continue the conversation below.\n", session.Restored.Title)
	}
	return session, bases, true
}

// remoteSearch implements /search over the daemon: it parses the optional
// "-k N" flag and query terms with the same parser as the direct REPL, then
// POSTs to /1.0/search with the currently active bases. The daemon holds the
// embedding model and runs the hybrid pipeline server-side; the REPL only
// renders the returned hits.
func remoteSearch(ctx context.Context, dc *apiclient.Client, args string, activeBases []string) {
	k, terms, ok := parseSearchArgs(args)
	if !ok {
		fmt.Println(searchUsage)
		return
	}
	if len(activeBases) == 0 {
		fmt.Printf("No active knowledge bases. Select one with %s first.\n", cmdUseKnowledge)
		return
	}

	stop := common.StartProgressSpinner("Searching")
	hits, err := dc.Search(ctx, terms, activeBases, k)
	stop()
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}
	if len(hits) == 0 {
		fmt.Println("No results found.")
		return
	}
	fmt.Print(formatRemoteSearchResults(hits))
}

// formatRemoteSearchResults renders daemon search hits for human reading,
// mirroring formatSearchResults but sourcing the base name and provenance from
// the API response (which already resolved them server-side). Hits are sorted
// by score descending by the daemon.
func formatRemoteSearchResults(hits []apiclient.SearchHit) string {
	var b strings.Builder
	for i, hit := range hits {
		if i > 0 {
			b.WriteString("\n")
		}

		header := fmt.Sprintf("[%d] score %.4f  ·  %s  %s", i+1, hit.Score, hit.Base, remoteProvenanceLabel(hit.Provenance))
		fmt.Fprintln(&b, color.New(color.Bold).Sprint(header))
		fmt.Fprintf(&b, "    source: %s   created: %s\n", hit.SourceID, hit.CreatedAt)
		fmt.Fprintln(&b, color.HiBlackString("    "+strings.Repeat("─", 56)))
		b.WriteString(hit.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// remoteProvenanceLabel maps the daemon's lowercase provenance tag to the same
// bracketed label the direct REPL prints via sourceLabel.
func remoteProvenanceLabel(provenance string) string {
	if strings.EqualFold(provenance, "upstream") {
		return "[UPSTREAM]"
	}
	return "[CANONICAL]"
}

// remotePromptTurn sends one prompt and renders streamed frames until the
// terminal "done" frame, colouring <think> content like the direct REPL. A
// spinner covers the server-side retrieval phase (query rewrite + search) that
// precedes the first token, so the wait shows activity like direct mode does;
// it is stopped as soon as the first frame arrives.
func remotePromptTurn(ctx context.Context, session *apiclient.ChatSession, prompt string) error {
	if err := session.Prompt(ctx, prompt); err != nil {
		return err
	}

	stop := common.StartProgressSpinner("Thinking")
	stopped := false
	haltSpinner := func() {
		if !stopped {
			stop()
			stopped = true
		}
	}
	defer haltSpinner()

	for {
		msg, err := session.Read(ctx)
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}
		switch msg.Type {
		case string(TokenAnswer):
			haltSpinner()
			fmt.Print(msg.Content)
		case string(TokenThink):
			haltSpinner()
			fmt.Print(color.BlueString(msg.Content))
		case "done":
			haltSpinner()
			fmt.Println()
			return nil
		case "error":
			haltSpinner()
			fmt.Println()
			return fmt.Errorf("%s", msg.Error)
		}
	}
}
