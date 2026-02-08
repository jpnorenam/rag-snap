package basic

import (
	"fmt"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type chatCommand struct {
	*common.Context
}

func ChatCommand(ctx *common.Context) *cobra.Command {
	var cmd chatCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:               "chat",
		Short:             "Start the chat CLI",
		Long:              "Chat with the server via its OpenAI API.\nThis CLI supports text-based prompting only.",
		GroupID:           groupID,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE:              cmd.run,
	}

	addDebugFlags(cobraCmd, ctx)

	return cobraCmd
}

func (cmd *chatCommand) run(_ *cobra.Command, _ []string) error {
	apiUrls, err := serverApiUrls(cmd.Context)
	if err != nil {
		return fmt.Errorf("error getting server api urls: %w", err)
	}

	knowledgeClient, err := knowledge.NewClient(apiUrls[opensearch])
	if err != nil {
		if cmd.Verbose {
			fmt.Printf("Knowledge base not available: %v\n", err)
		}
	}

	embeddingModelID, _ := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)

	return chat.Client(apiUrls[openAi], knowledgeClient, embeddingModelID, "", cmd.Verbose)
}
