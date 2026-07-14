package basic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
	"github.com/spf13/cobra"
)

type promptCommand struct {
	*common.Context
}

// PromptCommand builds the `prompt` command tree, which manages the system
// prompts the RAG pipeline runs on.
func PromptCommand(ctx *common.Context) *cobra.Command {
	var cmd promptCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:     "prompt",
		Aliases: []string{"p"},
		Short:   "Manage system prompt configuration",
		Long: "Manage system prompts used by the RAG pipeline.\n" +
			"Customized prompts override the built-in defaults.\n\n" +
			"When the ragd daemon is running, prompts are stored by the daemon and shared with\n" +
			"the web UI, so chat sessions and batch runs the daemon executes use them.\n" +
			"Without a daemon, prompts are read from ~/.config/rag-cli/prompts.json.",
		GroupID: groupID,
	}

	addDebugFlags(cobraCmd, ctx)
	cobraCmd.AddCommand(cmd.initCommand())

	return cobraCmd
}

func (cmd *promptCommand) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Configure system prompts interactively",
		Long: "Select one of the three RAG system prompts and edit it in the terminal.\n" +
			"The editor is pre-populated with the current value (custom or default);\n" +
			"a customized prompt can also be reset to its built-in default.\n\n" +
			"With the ragd daemon running, the prompt is saved to the daemon, so chat sessions,\n" +
			"batch runs, and the web UI all use it. Otherwise it is saved to\n" +
			"~/.config/rag-cli/prompts.json, which only direct (daemonless) CLI runs read.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if dc := daemonClient(cmd.Context); dc != nil {
				return runPromptInitDaemon(dc)
			}
			return runPromptInitLocal()
		},
	}
}

type promptKey string

const (
	keySourceRules        promptKey = "source_rules"
	keyAnswerSystemPrompt promptKey = "answer_system_prompt"
	keyChatSystemPrompt   promptKey = "chat_system_prompt"
)

// promptDescriptions labels each prompt in the select, matching the daemon's
// prompt names.
var promptDescriptions = map[promptKey]string{
	keySourceRules:        "source_rules — grounding constraints appended to custom batch prompts (answer batch)",
	keyAnswerSystemPrompt: "answer_system_prompt — system instruction for batch Q&A mode (answer batch)",
	keyChatSystemPrompt:   "chat_system_prompt — system instruction for the interactive chat REPL (chat)",
}

// selectOrder is the order prompts are offered in. It matches the daemon's
// canonical order so the CLI and the web UI present the same list.
var selectOrder = []promptKey{keyChatSystemPrompt, keyAnswerSystemPrompt, keySourceRules}

// runPromptInitDaemon edits prompts held by the daemon, which is what chat
// sessions, batch runs, and the web UI actually use. It offers to carry over any
// customizations left in the legacy client-local file first.
func runPromptInitDaemon(dc *apiclient.Client) error {
	ctx := context.Background()

	prompts, err := dc.ListPrompts(ctx)
	if err != nil {
		return fmt.Errorf("reading prompts from the daemon: %w", err)
	}

	if migrated, err := offerLegacyMigration(ctx, dc, prompts); err != nil {
		return err
	} else if migrated {
		// Re-read so the select reflects what was just carried over.
		if prompts, err = dc.ListPrompts(ctx); err != nil {
			return fmt.Errorf("reading prompts from the daemon: %w", err)
		}
	}

	selected, ok, err := selectPrompt(prompts)
	if err != nil || !ok {
		return err
	}

	current := findPrompt(prompts, selected)
	if current == nil {
		return fmt.Errorf("daemon does not know the prompt %q", selected)
	}

	// A customized prompt can be edited or put back to its built-in default.
	if current.Customized {
		reset, ok, err := askResetOrEdit(selected)
		if err != nil || !ok {
			return err
		}
		if reset {
			if _, err := dc.ResetPrompt(ctx, string(selected)); err != nil {
				return fmt.Errorf("resetting %s: %w", selected, err)
			}
			fmt.Printf("%s reset to its built-in default. New chats and batch runs will use it.\n", selected)
			return nil
		}
	}

	edited, ok, err := editPrompt(selected, current.Value)
	if err != nil || !ok {
		return err
	}
	if edited == current.Value {
		fmt.Println("No changes made.")
		return nil
	}

	if _, err := dc.SetPrompt(ctx, string(selected), edited); err != nil {
		return fmt.Errorf("saving %s: %w", selected, err)
	}
	fmt.Printf("%s saved to the daemon. New chats and batch runs will use it.\n", selected)
	return nil
}

// offerLegacyMigration carries customizations from the client-local prompts file
// over to the daemon, which cannot read that file itself (it runs as a service,
// with its own $HOME). Only prompts the daemon still has at their default are
// offered, so this cannot clobber a prompt already customized daemon-side, and
// nothing is copied without an explicit confirmation.
func offerLegacyMigration(ctx context.Context, dc *apiclient.Client, prompts []apiclient.Prompt) (bool, error) {
	local := chat.LoadPrompts()
	defaults := chat.DefaultPrompts()

	var candidates []promptKey
	for _, key := range selectOrder {
		localValue := localPromptValue(local, key)
		if localValue == "" || localValue == localPromptValue(defaults, key) {
			continue // Not customized locally.
		}
		if p := findPrompt(prompts, key); p != nil && !p.Customized {
			candidates = append(candidates, key)
		}
	}
	if len(candidates) == 0 {
		return false, nil
	}

	names := make([]string, len(candidates))
	for i, key := range candidates {
		names[i] = string(key)
	}

	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Carry your CLI prompt customizations over to the daemon?").
				Description(fmt.Sprintf(
					"These prompts are customized in ~/.config/rag-cli/prompts.json, which the daemon\n"+
						"cannot read, so chat and batch runs are NOT using them: %s.\n"+
						"Re-saving copies them to the daemon (and the web UI).",
					strings.Join(names, ", "))).
				Affirmative("Yes, re-save them").
				Negative("No, leave them").
				Value(&confirmed),
		),
	)
	// Aborting the offer (Ctrl+C / Esc) declines it: nothing is copied, and the
	// offer comes back next time while the divergence persists.
	if ok, err := runForm(form); err != nil {
		return false, err
	} else if !ok || !confirmed {
		return false, nil
	}

	for _, key := range candidates {
		if _, err := dc.SetPrompt(ctx, string(key), localPromptValue(local, key)); err != nil {
			return false, fmt.Errorf("re-saving %s to the daemon: %w", key, err)
		}
		fmt.Printf("Copied %s to the daemon.\n", key)
	}
	return true, nil
}

// runPromptInitLocal is the daemonless path: it edits the client-local prompts
// file, which only direct (non-daemon) CLI runs read.
func runPromptInitLocal() error {
	current := chat.LoadPrompts()

	// Present the same list as the daemon path, with no customized/default state
	// (the local file has no notion of an override).
	prompts := make([]apiclient.Prompt, 0, len(selectOrder))
	for _, key := range selectOrder {
		prompts = append(prompts, apiclient.Prompt{
			Name:  string(key),
			Value: localPromptValue(current, key),
		})
	}

	selected, ok, err := selectPrompt(prompts)
	if err != nil || !ok {
		return err
	}
	currentText := localPromptValue(current, selected)

	edited, ok, err := editPrompt(selected, currentText)
	if err != nil || !ok {
		return err
	}
	if edited == currentText {
		fmt.Println("No changes made.")
		return nil
	}

	setLocalPromptValue(&current, selected, edited)
	if err := chat.SavePrompts(current); err != nil {
		return fmt.Errorf("saving prompts: %w", err)
	}

	fmt.Printf("%s updated and saved to ~/.config/rag-cli/prompts.json\n", selected)
	fmt.Println("Note: this file is only read by direct CLI runs. With the ragd daemon running," +
		"\nrun `prompt init` again to save prompts where chat, batch runs, and the UI use them.")
	return nil
}

// selectPrompt asks which prompt to configure. ok is false when the user
// cancelled.
func selectPrompt(prompts []apiclient.Prompt) (selected promptKey, ok bool, err error) {
	options := make([]huh.Option[promptKey], 0, len(selectOrder))
	for _, key := range selectOrder {
		label := promptDescriptions[key]
		if p := findPrompt(prompts, key); p != nil && p.Customized {
			label += "  [customized]"
		}
		options = append(options, huh.NewOption(label, key))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[promptKey]().
				Title("Which prompt do you want to configure?").
				Options(options...).
				Value(&selected),
		),
	)
	ok, err = runForm(form)
	if err != nil || !ok {
		return "", false, err
	}
	return selected, true, nil
}

// runForm runs an interactive form and separates the two ways it can end without
// a submission: the user aborting (Ctrl+C / Esc), which is not an error and
// reports ok=false, and a genuine failure to render or read the form, which is
// returned so it cannot be mistaken for a cancellation.
func runForm(form *huh.Form) (ok bool, err error) {
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// askResetOrEdit offers to reset a customized prompt instead of editing it.
// ok is false when the user cancelled.
func askResetOrEdit(key promptKey) (reset bool, ok bool, err error) {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[bool]().
				Title(fmt.Sprintf("%s is customized. What do you want to do?", key)).
				Options(
					huh.NewOption("Edit the current prompt", false),
					huh.NewOption("Reset it to the built-in default", true),
				).
				Value(&reset),
		),
	)
	ok, err = runForm(form)
	if err != nil || !ok {
		return false, false, err
	}
	return reset, true, nil
}

// editPrompt opens the multiline editor pre-populated with the current value.
// ok is false when the user cancelled.
func editPrompt(key promptKey, current string) (edited string, ok bool, err error) {
	edited = current
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title(fmt.Sprintf("Edit: %s", key)).
				Description("Modify the prompt below. Submit with Alt+Enter or Ctrl+J. Press Esc to cancel.").
				Lines(12).
				Value(&edited),
		),
	)
	ok, err = runForm(form)
	if err != nil || !ok {
		return "", false, err
	}
	return edited, true, nil
}

// findPrompt returns the named prompt from a daemon listing, or nil.
func findPrompt(prompts []apiclient.Prompt, key promptKey) *apiclient.Prompt {
	for i := range prompts {
		if prompts[i].Name == string(key) {
			return &prompts[i]
		}
	}
	return nil
}

// localPromptValue reads one prompt out of the client-local config by name.
func localPromptValue(cfg chat.PromptConfig, key promptKey) string {
	switch key {
	case keySourceRules:
		return cfg.SourceRules
	case keyAnswerSystemPrompt:
		return cfg.AnswerSystemPrompt
	case keyChatSystemPrompt:
		return cfg.ChatSystemPrompt
	}
	return ""
}

// setLocalPromptValue writes one prompt into the client-local config by name.
func setLocalPromptValue(cfg *chat.PromptConfig, key promptKey, value string) {
	switch key {
	case keySourceRules:
		cfg.SourceRules = value
	case keyAnswerSystemPrompt:
		cfg.AnswerSystemPrompt = value
	case keyChatSystemPrompt:
		cfg.ChatSystemPrompt = value
	}
}
