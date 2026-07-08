package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/chzyer/readline"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

const (
	cmdUseKnowledge = "/use-knowledge"
	cmdUseKapa      = "/use-kapa"
)

// slashCommands lists every registered slash command name.
var slashCommands = []string{cmdUseKnowledge, cmdUseKapa}

// slashHinter returns a readline listener that displays matching slash
// commands below the input line as the user types, filtering the list
// with each keystroke.
func slashHinter() readline.Listener {
	var hinting bool
	return readline.FuncListener(func(line []rune, pos int, key rune) ([]rune, int, bool) {
		input := string(line)

		if strings.HasPrefix(input, "/") {
			// Clear previous hints, then draw updated ones.
			fmt.Fprint(os.Stderr, "\033[s\n\033[J\033[u")

			var matches []string
			for _, cmd := range slashCommands {
				if strings.HasPrefix(cmd, input) && cmd != input {
					matches = append(matches, cmd)
				}
			}
			if len(matches) > 0 {
				fmt.Fprint(os.Stderr, "\033[s")
				for _, m := range matches {
					fmt.Fprintf(os.Stderr, "\n  \033[90m%s\033[0m", m)
				}
				fmt.Fprint(os.Stderr, "\033[u")
			}
			hinting = true
		} else if hinting {
			// Leaving slash mode — clear leftover hints once.
			fmt.Fprint(os.Stderr, "\033[s\n\033[J\033[u")
			hinting = false
		}

		return line, pos, false
	})
}

// clearSlashHints removes any slash command hints displayed below the
// current input line. Safe to call even when no hints are showing.
func clearSlashHints() {
	fmt.Fprint(os.Stderr, "\033[s\n\033[J\033[u")
}

// Session holds the mutable state for a chat session, including connected
// clients and user-selected configuration. It is passed to slash-command
// handlers so they can read and modify session state.
type Session struct {
	KnowledgeClient  *knowledge.OpenSearchClient
	KapaClient       *knowledge.KapaClient
	EmbeddingModelID string
	ActiveIndexes    []string
	ActiveKapaGroups []string
}

// handleSlashCommand processes slash commands entered in the chat REPL.
// Returns true if the command was recognized.
func handleSlashCommand(input string, session *Session) bool {
	cmd := strings.TrimSpace(input)

	switch {
	case cmd == cmdUseKnowledge:
		if err := selectActiveContext(session); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		return true
	case cmd == cmdUseKapa:
		if session.KapaClient == nil {
			fmt.Println("Kapa is not configured. Set kapa.api.key and kapa.project.id.")
		} else {
			if err := selectKapaGroups(session); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		}
		return true
	default:
		fmt.Printf("Unknown command: %s\nAvailable commands: %s\n", cmd, strings.Join(slashCommands, ", "))
		return false
	}
}

// selectActiveContext lists knowledge base indexes and presents an interactive
// multi-select menu for the user to choose which knowledge bases should be
// active for the current chat session.
func selectActiveContext(session *Session) error {
	if session.KnowledgeClient == nil {
		return fmt.Errorf("knowledge base not available")
	}

	stop := common.StartProgressSpinner("Fetching knowledge bases")
	indexes, err := session.KnowledgeClient.ListIndexes(context.Background())
	stop()
	if err != nil {
		return fmt.Errorf("listing knowledge bases: %w", err)
	}

	if len(indexes) == 0 {
		fmt.Println("No knowledge bases found. Create one with 'knowledge create <name>'.")
		return nil
	}

	// Build selection options from available indexes.
	options := make([]huh.Option[string], len(indexes))
	for i, idx := range indexes {
		name, _ := knowledge.KnowledgeBaseNameFromIndex(idx.Name)
		label := fmt.Sprintf("%s (%s docs, %s)", name, idx.DocsCount, idx.StoreSize)
		options[i] = huh.NewOption(label, idx.Name)
	}

	// Pre-select any currently active indexes.
	selected := make([]string, len(session.ActiveIndexes))
	copy(selected, session.ActiveIndexes)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select active knowledge bases").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		if !errors.Is(err, huh.ErrUserAborted) {
			return err
		}
		return nil
	}

	session.ActiveIndexes = selected

	return nil
}

// selectKapaGroups fetches available Kapa source groups and presents an
// interactive multi-select menu. Selecting no groups disables Kapa retrieval.
// session.ActiveKapaGroups stores source group IDs (not names) for the API call.
func selectKapaGroups(session *Session) error {
	stop := common.StartProgressSpinner("Fetching Kapa source groups")
	groups, err := session.KapaClient.ListSourceGroups(context.Background())
	stop()
	if err != nil {
		return fmt.Errorf("listing Kapa source groups: %w", err)
	}

	if len(groups) == 0 {
		fmt.Println("No Kapa source groups found.")
		return nil
	}

	// Display Name, store ID — the retrieval API filters by ID.
	options := make([]huh.Option[string], len(groups))
	for i, g := range groups {
		options[i] = huh.NewOption(g.Name, g.ID)
	}

	selected := make([]string, len(session.ActiveKapaGroups))
	copy(selected, session.ActiveKapaGroups)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select active Kapa source groups").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		if !errors.Is(err, huh.ErrUserAborted) {
			return err
		}
		return nil
	}

	if len(selected) == 0 {
		session.ActiveKapaGroups = nil
		fmt.Println("Kapa knowledge disabled.")
	} else {
		session.ActiveKapaGroups = selected
		fmt.Printf("Kapa knowledge active — %d source group(s) selected.\n", len(selected))
	}

	return nil
}
