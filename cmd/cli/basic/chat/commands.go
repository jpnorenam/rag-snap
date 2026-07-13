package chat

import (
	"context"
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
	cmdSearch       = "/search"
)

// slashCommand describes a registered slash command and its argument syntax.
type slashCommand struct {
	name   string // e.g. "/search"
	syntax string // e.g. "[-k N] <query>"; empty when the command takes no args
}

// slashCommands lists every registered slash command.
var slashCommands = []slashCommand{
	{name: cmdUseKnowledge},
	{name: cmdSearch, syntax: "[-k N] <query>"},
}

// syntaxHint returns the argument syntax to show as dimmed ghost text when
// input is a recognized command still awaiting its arguments. The second
// return value is false when nothing should be shown (unknown command, a
// command without args, or once the user has started typing the arguments).
func syntaxHint(input string) (string, bool) {
	trimmed := strings.TrimRight(input, " ")
	for _, c := range slashCommands {
		if c.syntax != "" && trimmed == c.name {
			return c.syntax, true
		}
	}
	return "", false
}

// syntaxPainter is a readline Painter that renders a command's argument syntax
// as dimmed inline ghost text after the cursor, shell-autosuggest style.
type syntaxPainter struct{}

// Paint appends dimmed ghost text after the input, then moves the cursor back
// to sit right after the user's text. readline's own cursor-position logic only
// walks back over the real buffer length (ignoring runes Paint appends), so with
// the cursor at end-of-line the trailing CSI-D is what restores the cursor.
func (syntaxPainter) Paint(line []rune, pos int) []rune {
	if pos != len(line) {
		// Only ghost when the cursor is at end-of-line; gating here avoids
		// tangling with readline's mid-line backspace sequence.
		return line
	}
	suffix, ok := syntaxHint(string(line))
	if !ok {
		return line
	}
	ghost := " " + suffix
	w := len([]rune(ghost)) // syntax is ASCII, so rune count == display width
	out := append([]rune{}, line...)
	out = append(out, []rune("\033[90m"+ghost+"\033[0m")...)
	out = append(out, []rune(fmt.Sprintf("\033[%dD", w))...)
	return out
}

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
			for _, c := range slashCommands {
				if strings.HasPrefix(c.name, input) && c.name != input {
					matches = append(matches, c.name)
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
	EmbeddingModelID string
	ActiveIndexes    []string
}

// handleSlashCommand processes slash commands entered in the chat REPL.
// Returns true if the command was recognized.
func handleSlashCommand(input string, session *Session) bool {
	verb, args, _ := strings.Cut(strings.TrimSpace(input), " ")

	switch verb {
	case cmdUseKnowledge:
		if err := selectActiveContext(session); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		return true
	case cmdSearch:
		handleSearch(args, session)
		return true
	default:
		names := make([]string, len(slashCommands))
		for i, c := range slashCommands {
			names[i] = c.name
		}
		fmt.Printf("Unknown command: %s\nAvailable commands: %s\n", verb, strings.Join(names, ", "))
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
		// User cancelled (Ctrl+C / Esc) — keep existing context.
		return nil
	}

	session.ActiveIndexes = selected

	return nil
}
