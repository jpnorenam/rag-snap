package basic

import (
	"fmt"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type knowledgeCommand struct {
	*common.Context
}

func (cmd *knowledgeCommand) opensearchURL() (string, error) {
	apiUrls, err := serverApiUrls(cmd.Context)
	if err != nil {
		return "", fmt.Errorf("getting server API URLs: %w", err)
	}
	return apiUrls[opensearch], nil
}

func KnowledgeCommand(ctx *common.Context) *cobra.Command {
	var cmd knowledgeCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:     "knowledge",
		Short:   "Manage knowledge base",
		Long:    "Manage the OpenSearch knowledge base for RAG.\nSupports initializing pipelines, creating indices, ingesting documents, searching, and removing documents.",
		GroupID: groupID,
	}

	cobraCmd.AddCommand(
		cmd.initCommand(),
		cmd.listCommand(),
		cmd.createCommand(),
		cmd.ingestCommand(),
		cmd.searchCommand(),
		cmd.forgetCommand(),
	)

	return cobraCmd
}

func (cmd *knowledgeCommand) initCommand() *cobra.Command {
	var sentenceTransformer string
	var crossEncoder string

	cobraCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the knowledge base pipelines and index template",
		Long:  "Create and initialize an OpenSearch pipelines and index template for storing knowledge base documents.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if sentenceTransformer != "" {
				fmt.Printf("  Sentence transformer model: %s\n", sentenceTransformer)
			}
			if crossEncoder != "" {
				fmt.Printf("  Cross-encoder model: %s\n", crossEncoder)
			}

			url, err := cmd.opensearchURL()
			if err != nil {
				return err
			}

			return knowledge.Client(url, true)
		},
	}

	cobraCmd.Flags().StringVarP(&sentenceTransformer, "sentence-transformer", "s", "", "Sentence transformer model name")
	cobraCmd.Flags().StringVarP(&crossEncoder, "cross-encoder", "c", "", "Cross-encoder model name")

	return cobraCmd
}

func (cmd *knowledgeCommand) listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List knowledge base indexes",
		Long:  "List all OpenSearch indexes matching the knowledge base pattern.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			url, err := cmd.opensearchURL()
			if err != nil {
				return err
			}

			indexes, err := knowledge.ListIndexes(url)
			if err != nil {
				return fmt.Errorf("listing indexes: %w", err)
			}

			if len(indexes) == 0 {
				fmt.Println("No knowledge base indexes found.")
				return nil
			}

			fmt.Printf("%-30s %-10s %-10s %-12s %-10s\n", "INDEX", "HEALTH", "STATUS", "DOCS", "SIZE")
			for _, idx := range indexes {
				fmt.Printf("%-30s %-10s %-10s %-12s %-10s\n",
					idx.Name, idx.Health, idx.Status, idx.DocsCount, idx.StoreSize)
			}

			return nil
		},
	}
}

func (cmd *knowledgeCommand) createCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create <index_name>",
		Short: "Create a knowledge base index",
		Long:  "Create an OpenSearch index for storing knowledge base documents.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			indexName := args[0]

			url, err := cmd.opensearchURL()
			if err != nil {
				return err
			}

			if err := knowledge.CreateIndex(url, indexName); err != nil {
				return fmt.Errorf("creating index: %w", err)
			}

			fmt.Printf("Index '%s' created successfully.\n", indexName)
			return nil
		},
	}
}

func (cmd *knowledgeCommand) ingestCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ingest <index_name> <file_path> <metadata_id>",
		Short: "Ingest a document into the knowledge base",
		Long:  "Ingest a document from the specified file path into the OpenSearch index with the given metadata ID.",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			indexName := args[0]
			filePath := args[1]
			metadataID := args[2]
			fmt.Printf("[MOCK] Ingesting document into index: %s\n", indexName)
			fmt.Printf("  File: %s\n", filePath)
			fmt.Printf("  Metadata ID: %s\n", metadataID)
			return nil
		},
	}
}

func (cmd *knowledgeCommand) searchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search <index_name> <query>",
		Short: "Search the knowledge base",
		Long:  "Search for documents in the OpenSearch index matching the query text.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			indexName := args[0]
			query := args[1]
			fmt.Printf("[MOCK] Searching index: %s\n", indexName)
			fmt.Printf("  Query: %s\n", query)
			return nil
		},
	}
}

func (cmd *knowledgeCommand) forgetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <index_name> <metadata_id>",
		Short: "Remove documents from the knowledge base",
		Long:  "Remove all documents with the specified metadata ID from the OpenSearch index.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			indexName := args[0]
			metadataID := args[1]
			fmt.Printf("[MOCK] Forgetting documents from index: %s\n", indexName)
			fmt.Printf("  Metadata ID: %s\n", metadataID)
			return nil
		},
	}
}
