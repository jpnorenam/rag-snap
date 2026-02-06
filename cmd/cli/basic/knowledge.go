package basic

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

const (
	// dateFormat matches the OpenSearch index mapping format.
	knowledgeDateFormat = "2006-01-02 15:04:05"
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

	addDebugFlags(cobraCmd, ctx)

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
	var showSources bool

	cobraCmd := &cobra.Command{
		Use:   "list [index_name]",
		Short: "List knowledge base indexes or sources",
		Long:  "List all OpenSearch indexes matching the knowledge base pattern.\nUse --sources to list ingested source documents instead.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			url, err := cmd.opensearchURL()
			if err != nil {
				return err
			}

			if showSources {
				var indexFilter string
				if len(args) > 0 {
					indexFilter = args[0]
				}

				sources, err := knowledge.ListSourceMetadata(url, indexFilter)
				if err != nil {
					return fmt.Errorf("listing sources: %w", err)
				}

				if len(sources) == 0 {
					fmt.Println("No ingested sources found.")
					return nil
				}

				fmt.Printf("%-20s %-30s %-12s %-8s %-20s\n", "SOURCE ID", "FILE NAME", "STATUS", "CHUNKS", "INGESTED AT")
				for _, s := range sources {
					fmt.Printf("%-20s %-30s %-12s %-8d %-20s\n",
						s.SourceID, s.FileName, s.Status, s.ChunkCount, s.IngestedAt)
				}

				return nil
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

	cobraCmd.Flags().BoolVarP(&showSources, "sources", "s", false, "List ingested source documents instead of indexes")

	return cobraCmd
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
		Use:   "ingest <index_name> <file_path> <source_id>",
		Short: "Ingest a document into the knowledge base",
		Long:  "Ingest a document from the specified file path into the OpenSearch index with the given source ID.",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			indexName := args[0]
			filePath := args[1]
			sourceID := args[2]

			apiUrls, err := serverApiUrls(cmd.Context)
			if err != nil {
				return fmt.Errorf("getting server API URLs: %w", err)
			}

			result, err := processing.Ingest(apiUrls[tika], filePath, sourceID)
			if err != nil {
				return fmt.Errorf("ingesting document: %w", err)
			}

			// Build source metadata with status=processing
			now := time.Now().UTC().Format(knowledgeDateFormat)
			meta := knowledge.SourceMetadata{
				SourceID:      sourceID,
				FileName:      filepath.Base(filePath),
				FilePath:      filePath,
				Checksum:      result.Checksum,
				IndexName:     indexName,
				ChunkCount:    len(result.Chunks),
				ChunkSize:     processing.DefaultChunkSize,
				ChunkOverlap:  processing.DefaultChunkOverlap,
				ContentLength: result.ContentLength,
				Status:        knowledge.StatusProcessing,
				IngestedAt:    now,
				UpdatedAt:     now,
			}
			if result.TikaMetadata != nil {
				meta.ContentType = result.TikaMetadata.ContentType
				meta.Title = result.TikaMetadata.Title
				meta.Author = result.TikaMetadata.Author
				meta.Language = result.TikaMetadata.Language
			}

			// Write metadata BEFORE bulk indexing
			if err := knowledge.IndexSourceMetadata(apiUrls[opensearch], meta); err != nil {
				return fmt.Errorf("writing source metadata: %w", err)
			}

			// Convert chunks to documents and bulk index
			docs := make([]knowledge.Document, len(result.Chunks))
			for i, c := range result.Chunks {
				docs[i] = knowledge.Document{
					Content:   c.Content,
					SourceID:  c.SourceID,
					CreatedAt: c.CreatedAt,
				}
			}

			bulkResult, err := knowledge.BulkIndex(apiUrls[opensearch], indexName, docs)
			if err != nil {
				_ = knowledge.UpdateSourceStatus(apiUrls[opensearch], sourceID, knowledge.StatusFailed)
				return fmt.Errorf("indexing chunks: %w", err)
			}

			// Update metadata status to completed
			// Todo validate bulkResult errors == 0?
			if err := knowledge.UpdateSourceStatus(apiUrls[opensearch], sourceID, knowledge.StatusCompleted); err != nil {
				return fmt.Errorf("updating source status: %w", err)
			}

			fmt.Printf("Ingested %d/%d chunks into index '%s'\n",
				bulkResult.Indexed, bulkResult.Total, indexName)
			if bulkResult.Errors > 0 {
				fmt.Printf("  Errors: %d\n", bulkResult.Errors)
			}

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
		Use:   "forget <index_name> <source_id>",
		Short: "Remove a source and its chunks from the knowledge base",
		Long:  "Remove all chunks with the specified source ID from the OpenSearch index and delete the source metadata record.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			indexName := args[0]
			sourceID := args[1]

			url, err := cmd.opensearchURL()
			if err != nil {
				return err
			}

			// Verify source exists
			if _, err := knowledge.GetSourceMetadata(url, sourceID); err != nil {
				return fmt.Errorf("source not found: %w", err)
			}

			// Delete chunks from the KNN index
			deleted, err := knowledge.DeleteChunksBySourceID(url, indexName, sourceID)
			if err != nil {
				return fmt.Errorf("deleting chunks: %w", err)
			}

			// Delete the metadata record
			if err := knowledge.DeleteSourceMetadata(url, sourceID); err != nil {
				return fmt.Errorf("deleting source metadata: %w", err)
			}

			fmt.Printf("Deleted %d chunks and metadata for source '%s' from index '%s'\n",
				deleted, sourceID, indexName)

			return nil
		},
	}
}
