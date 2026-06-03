package basic

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type promptCommand struct {
	*common.Context
}

func PromptCommand(ctx *common.Context) *cobra.Command {
	var cmd promptCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:     "prompt",
		Aliases: []string{"p"},
		Short:   "Manage system prompt configuration",
		Long:    "Manage system prompts used by the RAG pipeline.\nCustomized prompts are saved to ~/.config/rag-cli/prompts.json and override built-in defaults.",
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
			"The editor is pre-populated with the current value (custom or default).\n" +
			"Saving writes the prompt to ~/.config/rag-cli/prompts.json.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPromptInit()
		},
	}
}

type promptKey string

const (
	keySourceRules        promptKey = "source_rules"
	keyAnswerSystemPrompt promptKey = "answer_system_prompt"
	keyChatSystemPrompt   promptKey = "chat_system_prompt"
)

func runPromptInit() error {
	current := chat.LoadPrompts()

	var selected promptKey

	selectForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[promptKey]().
				Title("Which prompt do you want to configure?").
				Options(
					huh.NewOption(
						"source_rules — grounding constraints appended to custom batch prompts (answer batch)",
						keySourceRules,
					),
					huh.NewOption(
						"answer_system_prompt — system instruction for batch Q&A mode (answer batch)",
						keyAnswerSystemPrompt,
					),
					huh.NewOption(
						"chat_system_prompt — system instruction for the interactive chat REPL (chat)",
						keyChatSystemPrompt,
					),
				).
				Value(&selected),
		),
	)

	if err := selectForm.Run(); err != nil {
		// User cancelled (Ctrl+C / Esc).
		return nil
	}

	var currentText string
	var label string
	switch selected {
	case keySourceRules:
		currentText = current.SourceRules
		label = "source_rules"
	case keyAnswerSystemPrompt:
		currentText = current.AnswerSystemPrompt
		label = "answer_system_prompt"
	case keyChatSystemPrompt:
		currentText = current.ChatSystemPrompt
		label = "chat_system_prompt"
	}

	edited := currentText

	editForm := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title(fmt.Sprintf("Edit: %s", label)).
				Description("Modify the prompt below. Submit with Alt+Enter or Ctrl+J. Press Esc to cancel.").
				Lines(12).
				Value(&edited),
		),
	)

	if err := editForm.Run(); err != nil {
		// User cancelled.
		return nil
	}

	if edited == currentText {
		fmt.Println("No changes made.")
		return nil
	}

	switch selected {
	case keySourceRules:
		current.SourceRules = edited
	case keyAnswerSystemPrompt:
		current.AnswerSystemPrompt = edited
	case keyChatSystemPrompt:
		current.ChatSystemPrompt = edited
	}

	if err := chat.SavePrompts(current); err != nil {
		return fmt.Errorf("saving prompts: %w", err)
	}

	fmt.Printf("%s updated and saved to ~/.config/rag-cli/prompts.json\n", label)
	return nil
}
