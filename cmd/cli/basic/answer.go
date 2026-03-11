package basic

import (
	"fmt"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type answerCommand struct {
	*common.Context
}

func AnswerCommand(ctx *common.Context) *cobra.Command {
	var cmd answerCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:     "answer",
		Aliases: []string{"a"},
		Short:   "Answer questions using the knowledge base",
		Long:    "Run structured question-answering sessions grounded in your knowledge bases.",
		GroupID: groupID,
	}

	cobraCmd.AddCommand(cmd.batchCommand())

	addDebugFlags(cobraCmd, ctx)

	return cobraCmd
}

func (cmd *answerCommand) batchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "batch <manifest.yaml>",
		Short: "Run multiple questions from a YAML manifest and export results to JSON",
		Long: "Reads a YAML manifest defining a list of questions, runs each through the RAG+LLM pipeline, " +
			"and writes the results to a timestamped JSON file.\n\n" +
			"An optional top-level 'prompt' field in the manifest overrides the default system prompt for the entire batch.",
		Args: cobra.ExactArgs(1),
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
