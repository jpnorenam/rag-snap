package basic

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
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
		cmd.exportCommand(),
		cmd.importCommand(),
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
	var batchFlag string
	var forceFlag bool

	cobraCmd := &cobra.Command{
		Use:   "ingest [<knowledge_base_name> <source_id>]",
		Short: "Ingest a document into the knowledge base",
		Long: "Ingest a document into the knowledge base index with the given source ID.\n" +
			"Provide the document via --file (local path) or --url (remote URL).\n" +
			"Use --batch <config.yaml> to ingest multiple documents from a YAML file.",
		Args: cobra.RangeArgs(0, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			// Batch mode: delegate to ProcessBatch, no positional args needed.
			if batchFlag != "" {
				if len(args) != 0 {
					return fmt.Errorf("positional arguments are not allowed with --batch")
				}
				apiUrls, err := serverApiUrls(cmd.Context)
				if err != nil {
					return fmt.Errorf("getting server API URLs: %w", err)
				}
				client, err := cmd.opensearchClient()
				if err != nil {
					return err
				}
				return knowledge.ProcessBatch(context.Background(), client, apiUrls[tika], batchFlag, forceFlag)
			}

			// Single-document mode: require exactly 2 positional args.
			if len(args) != 2 {
				return fmt.Errorf("requires <knowledge_base_name> and <source_id>, or use --batch <config.yaml>")
			}
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
			if err := client.UpdateSourceStatus(ctx, sourceID, knowledge.StatusCompleted); err != nil {
				return fmt.Errorf("updating source status: %w", err)
			}

			fmt.Printf("Ingested %d/%d chunks into index '%s'\n",
				bulkResult.Indexed, bulkResult.Total, indexName)
			if bulkResult.Errors > 0 {
				fmt.Printf("  Errors: %d (%s)\n", bulkResult.Errors, bulkResult.FirstError)
			}

			return nil
		},
	}

	cobraCmd.Flags().StringVarP(&fileFlag, "file", "f", "", "Local file path to ingest")
	cobraCmd.Flags().StringVarP(&urlFlag, "url", "u", "", "URL to download and ingest")
	cobraCmd.Flags().StringVarP(&batchFlag, "batch", "B", "", "YAML batch config file — ingest multiple documents at once")
	cobraCmd.Flags().BoolVar(&forceFlag, "force", false, "Re-ingest sources even if already present in the knowledge base")

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

func (cmd *knowledgeCommand) exportCommand() *cobra.Command {
	var outputDir string
	var compress bool

	cobraCmd := &cobra.Command{
		Use:   "export <kb-name>",
		Short: "Export a knowledge base to a directory",
		Long:  "Export all documents, mappings, and source metadata for a knowledge base using elasticdump.\nThe output directory contains data.json, mapping.json, sources.json, and manifest.json.\nUse --compress to produce a .tar.gz archive instead.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			kbName := args[0]

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			return knowledge.ExportKnowledgeBase(context.Background(), client, kbName, knowledge.ExportOptions{
				OutputDir: outputDir,
				Compress:  compress,
			})
		},
	}

	cobraCmd.Flags().StringVarP(&outputDir, "output", "o", "", "Output directory (default: ./<kb-name>-export)")
	cobraCmd.Flags().BoolVarP(&compress, "compress", "c", false, "Compress the export into a .tar.gz archive")

	return cobraCmd
}

func (cmd *knowledgeCommand) importCommand() *cobra.Command {
	var (
		inputDir string
		driveURL string
		kbName   string
		all      bool
		noAuth   bool
		force    bool
	)

	cobraCmd := &cobra.Command{
		Use:   "import [kb-name]",
		Short: "Import a knowledge base from an export directory, archive, or Google Drive",
		Long: "Restore a knowledge base from a directory or .tar.gz archive produced by 'knowledge export'.\n\n" +
			"Local import:\n" +
			"  --input <path>   directory or .tar.gz archive\n\n" +
			"Google Drive import:\n" +
			"  --url <gdrive-url>   Canonical-shared Drive folder or .tar.gz file link\n" +
			"  --all                import all archives without interactive selection\n" +
			"  --no-auth            skip OAuth; only works with a single publicly shared file URL\n\n" +
			"On first use with --url, you will be prompted to authenticate with your\n" +
			"Google account via a browser. The token is cached for subsequent runs.\n\n" +
			"If [kb-name] is omitted, the name stored in the export manifest is used.\n" +
			"Provide [kb-name] to restore under a different name.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// Exactly one source must be provided.
			if inputDir == "" && driveURL == "" {
				return fmt.Errorf("provide either --input <path> or --url <google-drive-url>")
			}
			if inputDir != "" && driveURL != "" {
				return fmt.Errorf("--input and --url are mutually exclusive")
			}

			// Positional kb-name (optional override).
			if len(args) > 0 {
				kbName = args[0]
			}

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			ctx := context.Background()

			// ── Local import ────────────────────────────────────────────────
			if inputDir != "" {
				return knowledge.ImportKnowledgeBase(ctx, client, kbName, knowledge.ImportOptions{
					InputDir: inputDir,
					Force:    force,
				})
			}

			// ── Google Drive import ──────────────────────────────────────────
			kind, resourceID, err := knowledge.ParseDriveURL(driveURL)
			if err != nil {
				return err
			}

			if noAuth && kind == knowledge.DriveKindFolder {
				fmt.Println("Note: --no-auth folder listing uses HTML scraping and may break if Google changes their page structure.")
			}

			var accessToken string
			if !noAuth {
				accessToken, err = knowledge.LoadOrAuthenticateDrive(ctx)
				if err != nil {
					return fmt.Errorf("Drive authentication: %w", err)
				}
			}

			var archives []knowledge.DriveArchive

			switch kind {
			case knowledge.DriveKindFolder:
				stop := common.StartProgressSpinner("Listing archives in Google Drive folder")
				if noAuth {
					archives, err = knowledge.ListPublicDriveArchives(ctx, resourceID)
				} else {
					archives, err = knowledge.ListDriveArchives(ctx, resourceID, accessToken)
				}
				stop()
				if err != nil {
					return fmt.Errorf("listing Drive archives: %w", err)
				}
				if len(archives) == 0 {
					fmt.Println("No .tar.gz archives found in the specified folder.")
					return nil
				}

				if !all {
					archives, err = selectDriveArchives(archives)
					if err != nil {
						return err
					}
					if len(archives) == 0 {
						fmt.Println("No archives selected.")
						return nil
					}
				}

			case knowledge.DriveKindFile:
				// Single file — no listing or selection needed.
				archives = []knowledge.DriveArchive{{ID: resourceID, Name: driveURL}}
			}

			for i, archive := range archives {
				fmt.Printf("[%d/%d] Downloading %s...\n", i+1, len(archives), archive.Name)
				var tmpPath string
				var cleanup func()
				// resolvedName holds the actual filename; for the OAuth path this is
				// already in archive.Name, but for --no-auth it comes from Content-Disposition.
				resolvedName := archive.Name
				if noAuth {
					var dlName string
					tmpPath, dlName, cleanup, err = knowledge.DownloadPublicDriveArchive(ctx, archive)
					if dlName != "" {
						resolvedName = dlName
					}
				} else {
					tmpPath, cleanup, err = knowledge.DownloadDriveArchive(ctx, archive, accessToken)
				}
				if err != nil {
					fmt.Printf("  skip: %v\n", err)
					continue
				}

				// Derive a KB name from the archive filename when none is provided.
				target := kbName
				if target == "" {
					target = archiveStem(resolvedName)
				}

				fmt.Printf("  Importing as knowledge base %q...\n", target)
				importErr := knowledge.ImportKnowledgeBase(ctx, client, target, knowledge.ImportOptions{
					InputDir: tmpPath,
					Force:    force,
				})
				cleanup()
				if importErr != nil {
					fmt.Printf("  error: %v\n", importErr)
				}
			}
			return nil
		},
	}

	cobraCmd.Flags().StringVarP(&inputDir, "input", "i", "", "Local directory or .tar.gz archive to import")
	cobraCmd.Flags().StringVarP(&driveURL, "url", "u", "", "Google Drive folder or file URL to import from")
	cobraCmd.Flags().BoolVar(&all, "all", false, "Import all archives from a Drive folder without prompting")
	cobraCmd.Flags().BoolVar(&noAuth, "no-auth", false, "Skip OAuth; use only with a single publicly shared file URL")
	cobraCmd.Flags().BoolVar(&force, "force", false, "Overwrite even if the target index is non-empty")

	return cobraCmd
}

// selectDriveArchives presents an interactive multi-select for a list of Drive archives.
func selectDriveArchives(archives []knowledge.DriveArchive) ([]knowledge.DriveArchive, error) {
	options := make([]huh.Option[int], len(archives))
	for i, a := range archives {
		label := a.Name
		if a.Size >= 0 {
			label = fmt.Sprintf("%s (%s)", a.Name, humanBytes(a.Size))
		}
		options[i] = huh.NewOption(label, i)
	}

	var chosen []int
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Select archives to import").
				Options(options...).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("selection cancelled: %w", err)
	}

	selected := make([]knowledge.DriveArchive, len(chosen))
	for i, idx := range chosen {
		selected[i] = archives[idx]
	}
	return selected, nil
}

// archiveStem strips the archive extension and the trailing "-export" suffix
// from a filename to derive a clean KB name (e.g. "mybase-export.tar.gz" → "mybase").
func archiveStem(name string) string {
	name = strings.TrimSuffix(name, ".tar.gz")
	name = strings.TrimSuffix(name, ".tgz")
	name = strings.TrimSuffix(name, "-export")
	return name
}

// humanBytes formats a byte count as a human-readable string (e.g. "12.4 MB").
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
