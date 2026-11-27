package debug

import (
	"fmt"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type chatCommand struct {
	*common.Context

	// flags
	baseUrl   string
	api       string
	modelName string
}

func ChatCommand(ctx *common.Context) *cobra.Command {
	var cmd chatCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:               "chat",
		Short:             "Start the chat CLI providing connection parameters",
		Long:              "Open a text-only chat session to the inference server at the provided URL (OpenAI-compatible, or Anthropic Messages with --api anthropic), requesting the model as specified.",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE:              cmd.run,
	}

	// flags
	cobraCmd.Flags().StringVar(&cmd.baseUrl, "base-url", "", "Base URL of the inference server")
	cobraCmd.Flags().StringVar(&cmd.modelName, "model", "", "Name of the model to use")
	cobraCmd.Flags().StringVar(&cmd.api, "api", chat.APIOpenAI, "Wire protocol of the server: openai or anthropic")

	return cobraCmd
}

func (cmd *chatCommand) run(_ *cobra.Command, args []string) error {
	if cmd.baseUrl == "" {
		return fmt.Errorf("the --base-url parameter is required")
	}

	return chat.Client(chat.BackendConfig{API: cmd.api, BaseURL: cmd.baseUrl}, nil, nil, "", cmd.modelName, chat.DefaultPrompts(), 0.3, cmd.Verbose)
}
