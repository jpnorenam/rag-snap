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
		Aliases:           []string{"c"},
		Short:             "Start the chat CLI",
		Long:              "Chat with the server via its OpenAI API.\nThis CLI supports text-based prompting only.",
		GroupID:           groupID,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE:              cmd.run,
	}

	cobraCmd.AddCommand(cmd.batchCommand())

	addDebugFlags(cobraCmd, ctx)

	return cobraCmd
}

func (cmd *chatCommand) batchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "batch <manifest.yaml>",
		Short: "Run multiple questions from a YAML manifest and export results to JSON",
		Long:  "Reads a YAML manifest defining a list of questions, runs each through the RAG+LLM pipeline, and writes the results to a timestamped JSON file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			manifest, err := chat.LoadBatchManifest(args[0])
			if err != nil {
				return fmt.Errorf("loading batch manifest: %w", err)
			}
			if manifest.Model == "" {
				manifest.Model, _ = getConfigString(cmd.Context, confChatModel)
			}
			apiUrls, err := serverApiUrls(cmd.Context)
			if err != nil {
				return fmt.Errorf("getting server API URLs: %w", err)
			}
			knowledgeClient, _ := knowledge.NewClient(apiUrls[opensearch])
			embeddingModelID, _ := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)
			return chat.ProcessBatchChat(apiUrls[openAi], knowledgeClient, embeddingModelID, manifest, cmd.Verbose)
		},
	}
}

func (cmd *chatCommand) run(_ *cobra.Command, args []string) error {
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

	var llmModelName string
	if len(args) > 0 {
		llmModelName = args[0]
	}
	if llmModelName == "" {
		llmModelName, _ = getConfigString(cmd.Context, confChatModel)
	}

	return chat.Client(apiUrls[openAi], knowledgeClient, embeddingModelID, llmModelName, cmd.Verbose)
}
