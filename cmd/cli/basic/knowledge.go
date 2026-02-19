package basic

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
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

// embeddingModelID retrieves the embedding model ID from the config store.
func (cmd *knowledgeCommand) embeddingModelID() (string, error) {
	modelID, err := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)
	if err != nil {
		return "", fmt.Errorf("embedding model ID not configured; run 'knowledge init' first")
	}
	return modelID, nil
}

// opensearchClient creates a new OpenSearch client for the configured cluster.
func (cmd *knowledgeCommand) opensearchClient() (*knowledge.OpenSearchClient, error) {
	url, err := cmd.opensearchURL()
	if err != nil {
		return nil, err
	}
	fmt.Printf("Using opensearch cluster at %v\n", url)
	return knowledge.NewClient(url)
}

func KnowledgeCommand(ctx *common.Context) *cobra.Command {
	var cmd knowledgeCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:     "knowledge",
		Aliases: []string{"k"},
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
		cmd.metadataCommand(),
		cmd.deleteCommand(),
		// New command added
		cmd.batchIngestCommand(),
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

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			return client.InitPipelines(context.Background())
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
			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			ctx := context.Background()

			if showSources {
				return cmd.listSources(ctx, client, args)
			}
			return cmd.listIndexes(ctx, client)
		},
	}

	cobraCmd.Flags().BoolVarP(&showSources, "sources", "s", false, "List ingested source documents instead of indexes")

	return cobraCmd
}

func (cmd *knowledgeCommand) createCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create <knowledge_base_name>",
		Short: "Create a knowledge base index",
		Long:  "Create an OpenSearch index for storing knowledge base documents.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			knowledgeBaseName := args[0]

			indexName := knowledge.FullIndexName(knowledgeBaseName)

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			if err := client.CreateIndex(context.Background(), indexName); err != nil {
				return fmt.Errorf("creating index: %w", err)
			}

			fmt.Printf("Knowledge base '%s' created successfully.\n", knowledgeBaseName)
			return nil
		},
	}
}

func (cmd *knowledgeCommand) ingestCommand() *cobra.Command {
	var fileFlag string
	var urlFlag string

	cobraCmd := &cobra.Command{
		Use:   "ingest <knowledge_base_name> <source_id>",
		Short: "Ingest a document into the knowledge base",
		Long:  "Ingest a document into the knowledge base index with the given source ID.\nProvide the document via --file (local path) or --url (remote URL).",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			knowledgeBaseName := args[0]
			sourceID := args[1]

			// Validate mutual exclusivity
			if fileFlag == "" && urlFlag == "" {
				return fmt.Errorf("either --file or --url must be specified")
			}
			if fileFlag != "" && urlFlag != "" {
				return fmt.Errorf("--file and --url are mutually exclusive")
			}

			// Resolve the file path
			var filePath string
			var metadataPath string // stored in SourceMetadata.FilePath
			var webMeta *processing.WebMetadata
			if urlFlag != "" {
				crawled, wm, cleanup, err := processing.CrawlURL(urlFlag)
				if err != nil {
					return fmt.Errorf("Crawling URL: %w", err)
				}
				defer cleanup()
				filePath = crawled
				metadataPath = urlFlag
				webMeta = wm
			} else {
				filePath = fileFlag
				metadataPath = fileFlag
			}

			indexName := knowledge.FullIndexName(knowledgeBaseName)

			apiUrls, err := serverApiUrls(cmd.Context)
			if err != nil {
				return fmt.Errorf("getting server API URLs: %w", err)
			}

			result, err := processing.Ingest(apiUrls[tika], filePath, sourceID)
			if err != nil {
				return fmt.Errorf("ingesting document: %w", err)
			}

			client, err := knowledge.NewClient(apiUrls[opensearch])
			if err != nil {
				return err
			}

			ctx := context.Background()

			// Build source metadata with status=processing
			now := time.Now().UTC().Format(knowledge.DateFormat)
			meta := knowledge.SourceMetadata{
				SourceID:      sourceID,
				FileName:      filepath.Base(filePath),
				FilePath:      metadataPath,
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
			if webMeta != nil {
				if meta.Title == "" {
					meta.Title = webMeta.Title
				}
				if meta.Author == "" {
					meta.Author = webMeta.Author
				}
			}

			// Write metadata BEFORE bulk indexing
			if err := client.IndexSourceMetadata(ctx, meta); err != nil {
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

			bulkResult, err := client.BulkIndex(ctx, indexName, docs)
			if err != nil {
				_ = client.UpdateSourceStatus(ctx, sourceID, knowledge.StatusFailed)
				return fmt.Errorf("indexing chunks: %w", err)
			}

			// Update metadata status to completed
			// Todo validate bulkResult errors == 0?
			if err := client.UpdateSourceStatus(ctx, sourceID, knowledge.StatusCompleted); err != nil {
				return fmt.Errorf("updating source status: %w", err)
			}

			fmt.Printf("Ingested %d/%d chunks into index '%s'\n",
				bulkResult.Indexed, bulkResult.Total, indexName)
			// CC: print OpenSearch error reason so failures are self-diagnosable
			if bulkResult.Errors > 0 {
				fmt.Printf("  Errors: %d (%s)\n", bulkResult.Errors, bulkResult.FirstError)
			}

			return nil
		},
	}

	cobraCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Local file path to ingest")
	cobraCmd.Flags().StringVarP(&urlFlag, "url", "u", "", "URL to download and ingest")

	return cobraCmd
}

func (cmd *knowledgeCommand) searchCommand() *cobra.Command {
	var (
		bases []string
		k     int
	)

	cobraCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the knowledge base",
		Long:  "Search for documents across knowledge bases.\nIf no bases are specified with --index, the default index is searched.\nResults from all bases are merged and sorted by relevance score.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			query := args[0]

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			// Retrieve the embedding model ID stored by 'knowledge init'.
			modelID, err := cmd.embeddingModelID()
			if err != nil {
				return err
			}

			// Resolve index names: use provided suffixes or default index.
			var fullIndexNames []string
			if len(bases) > 0 {
				for _, suffix := range bases {
					fullIndexNames = append(fullIndexNames, knowledge.FullIndexName(suffix))
				}
			} else {
				fullIndexNames = []string{knowledge.DefaultIndexName()}
			}

			results, err := client.Search(context.Background(), fullIndexNames, query, query, modelID, k)
			if err != nil {
				return fmt.Errorf("searching: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("No results found.")
				return nil
			}

			for i, hit := range results {
				fmt.Printf("\n--- Result %d (score: %.4f, index: %s) ---\n", i+1, hit.Score, hit.Index)
				fmt.Printf("  Source: %s\n", hit.SourceID)
				fmt.Printf("  Date:   %s\n", hit.CreatedAt)
				content := hit.Content
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				fmt.Printf("  %s\n", content)
			}

			fmt.Printf("\nTotal: %d results\n", len(results))
			return nil
		},
	}

	cobraCmd.Flags().StringSliceVarP(&bases, "bases", "b", nil, "Knowledge base name(s) to search (comma-separated string list, defaults to 'default')")
	cobraCmd.Flags().IntVarP(&k, "top", "k", 10, "Number of results per index")

	return cobraCmd
}

func (cmd *knowledgeCommand) forgetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <knowledge_base_name> <source_id>",
		Short: "Remove a source and its chunks from the knowledge base",
		Long:  "Remove all chunks with the specified source ID from the OpenSearch index and delete the source metadata record.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			knowledgeBaseName := args[0]
			sourceID := args[1]

			indexName := knowledge.FullIndexName(knowledgeBaseName)

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			ctx := context.Background()

			// Verify source exists
			if _, err := client.GetSourceMetadata(ctx, sourceID); err != nil {
				return fmt.Errorf("source not found: %w", err)
			}

			// Delete chunks from the KNN index
			deleted, err := client.DeleteChunksBySourceID(ctx, indexName, sourceID)
			if err != nil {
				return fmt.Errorf("deleting chunks: %w", err)
			}

			// Delete the metadata record
			if err := client.DeleteSourceMetadata(ctx, sourceID); err != nil {
				return fmt.Errorf("deleting source metadata: %w", err)
			}

			fmt.Printf("Deleted %d chunks and metadata for source '%s' from index '%s'\n",
				deleted, sourceID, indexName)

			return nil
		},
	}
}

func (cmd *knowledgeCommand) metadataCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "metadata <knowledge_base_name> <source_id>",
		Short: "Show metadata for an ingested source",
		Long:  "Display the stored metadata for a source document ingested into the knowledge base.",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			sourceID := args[1]

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			meta, err := client.GetSourceMetadata(context.Background(), sourceID)
			if err != nil {
				return fmt.Errorf("source not found: %w", err)
			}

			knowledgeBaseName, _ := knowledge.KnowledgeBaseNameFromIndex(meta.IndexName)

			fmt.Printf("Source ID:      %s\n", meta.SourceID)
			fmt.Printf("Knowledge base: %s\n", knowledgeBaseName)
			fmt.Printf("Status:         %s\n", meta.Status)
			fmt.Printf("File name:      %s\n", meta.FileName)
			fmt.Printf("File path:      %s\n", meta.FilePath)
			fmt.Printf("Content type:   %s\n", meta.ContentType)
			fmt.Printf("Content length: %d bytes\n", meta.ContentLength)
			fmt.Printf("Checksum:       %s\n", meta.Checksum)
			fmt.Printf("Chunks:         %d (size=%d, overlap=%d)\n", meta.ChunkCount, meta.ChunkSize, meta.ChunkOverlap)
			fmt.Printf("Ingested at:    %s\n", meta.IngestedAt)
			fmt.Printf("Updated at:     %s\n", meta.UpdatedAt)
			if meta.Title != "" {
				fmt.Printf("Title:          %s\n", meta.Title)
			}
			if meta.Author != "" {
				fmt.Printf("Author:         %s\n", meta.Author)
			}
			if meta.Language != "" {
				fmt.Printf("Language:       %s\n", meta.Language)
			}

			return nil
		},
	}
}

func (cmd *knowledgeCommand) deleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <knowledge_base_name>",
		Short: "Delete a knowledge base index and all its sources",
		Long:  "Delete an OpenSearch index and all associated source metadata records.\nRequires typing the knowledge base name to confirm.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			knowledgeBaseName := args[0]
			indexName := knowledge.FullIndexName(knowledgeBaseName)

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			ctx := context.Background()

			// Show the sources that will be deleted.
			sources, err := client.ListSourceMetadata(ctx, indexName)
			if err != nil {
				return fmt.Errorf("listing sources: %w", err)
			}

			if len(sources) == 0 {
				fmt.Printf("Knowledge base '%s' has no ingested sources.\n", knowledgeBaseName)
			} else {
				fmt.Printf("The following %d source(s) will be permanently deleted:\n\n", len(sources))
				fmt.Printf("  %-50s %-12s %-8s %-20s\n", "SOURCE ID", "STATUS", "CHUNKS", "INGESTED AT")
				for _, s := range sources {
					fmt.Printf("  %-50s %-12s %-8d %-20s\n", s.SourceID, s.Status, s.ChunkCount, s.IngestedAt)
				}
				fmt.Println()
			}

			// Confirmation prompt.
			fmt.Printf("This will permanently delete the index '%s' and all its data.\n", indexName)
			fmt.Printf("Type the knowledge base name to confirm: ")

			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading confirmation: %w", err)
			}
			input = strings.TrimSpace(input)

			if input != knowledgeBaseName {
				return fmt.Errorf("confirmation does not match — deletion aborted")
			}

			// Delete all source metadata records for this index.
			deleted, err := client.DeleteSourceMetadataByIndex(ctx, indexName)
			if err != nil {
				return fmt.Errorf("deleting source metadata: %w", err)
			}

			// Delete the index itself.
			if err := client.DeleteIndex(ctx, indexName); err != nil {
				return fmt.Errorf("deleting index: %w", err)
			}

			fmt.Printf("Deleted index '%s' and %d source metadata record(s).\n", indexName, deleted)
			return nil
		},
	}
}

// listIndexes lists all knowledge base indexes.
func (cmd *knowledgeCommand) listIndexes(ctx context.Context, client *knowledge.OpenSearchClient) error {
	indexes, err := client.ListIndexes(ctx)
	if err != nil {
		return fmt.Errorf("listing indexes: %w", err)
	}

	if len(indexes) == 0 {
		fmt.Println("No knowledge base indexes found.")
		return nil
	}

	fmt.Printf("%-30s %-10s %-10s %-12s %-10s\n", "KNOWLEDGE BASE", "HEALTH", "STATUS", "DOCS", "SIZE")
	for _, idx := range indexes {

		knowledgeBaseName, _ := knowledge.KnowledgeBaseNameFromIndex(idx.Name)
		fmt.Printf("%-30s %-10s %-10s %-12s %-10s\n",
			knowledgeBaseName, idx.Health, idx.Status, idx.DocsCount, idx.StoreSize)
	}

	return nil
}

// listSources lists all ingested source documents, optionally filtered by index name.
func (cmd *knowledgeCommand) listSources(ctx context.Context, client *knowledge.OpenSearchClient, args []string) error {
	var indexFilter string
	if len(args) > 0 {
		indexFilter = args[0]
	}

	if indexFilter != "" {
		indexFilter = knowledge.FullIndexName(indexFilter)
	}

	sources, err := client.ListSourceMetadata(ctx, indexFilter)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}

	if len(sources) == 0 {
		fmt.Println("No ingested sources found.")
		return nil
	}

	fmt.Printf("%-50s %-30s %-12s %-8s %-20s\n", "SOURCE ID", "KNOWLEDGE BASE", "STATUS", "CHUNKS", "INGESTED AT")
	for _, s := range sources {
		knowledgeBaseName, _ := knowledge.KnowledgeBaseNameFromIndex(s.IndexName)
		fmt.Printf("%-50s %-30s %-12s %-8d %-20s\n",
			s.SourceID, knowledgeBaseName, s.Status, s.ChunkCount, s.IngestedAt)
	}

	return nil
}

// Definition of the new batch ingest command, which processes multiple documents defined in a YAML config file.

func (cmd *knowledgeCommand) batchIngestCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ingest-batch [config.yaml]",
		Short: "Ingest multiple documents from a YAML configuration file",
		Long:  `Reads a YAML file defining a list of documents and ingests them into OpenSearch.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			yamlFile := args[0]
			ctx := context.Background() // O cmd.Context si prefieres heredar

			// 1. Get URLs from the server
			apiUrls, err := serverApiUrls(cmd.Context)
			if err != nil {
				return fmt.Errorf("getting server API URLs: %w", err)
			}
			tikaURL := apiUrls[tika] // 'tika' is defined in the common package as a constant key for retrieving the Tika URL.

			// 2. Initialize OpenSearch client
			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			// 3. Execute the batch processing function defined in the knowledge package, passing the client, Tika URL, and YAML file path.
			return knowledge.ProcessBatch(ctx, client, tikaURL, yamlFile)
		},
	}
}
