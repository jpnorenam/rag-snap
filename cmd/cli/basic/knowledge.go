package basic

import (
	"context"
	"fmt"
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
		cmd.labelCommand(),
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

			if dc := daemonClient(cmd.Context); dc != nil {
				opURL, err := dc.EngineInit(context.Background())
				if err != nil {
					return err
				}
				op, err := waitWithProgress(dc, opURL, "Initializing knowledge engine", "", "")
				if err != nil {
					return err
				}
				if embedding := op.MetadataString("embedding_model_id"); embedding != "" {
					fmt.Printf("Embedding model ID: %s\n", embedding)
				}
				if rerank := op.MetadataString("rerank_model_id"); rerank != "" {
					fmt.Printf("Rerank model ID: %s\n", rerank)
				}
				return nil
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
			ctx := context.Background()

			if dc := daemonClient(cmd.Context); dc != nil {
				if showSources {
					return cmd.listSourcesAPI(ctx, dc, args)
				}
				return cmd.listIndexesAPI(ctx, dc)
			}

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

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
	var labelFlag string

	cobraCmd := &cobra.Command{
		Use:   "create <knowledge_base_name>",
		Short: "Create a knowledge base index",
		Long: "Create an OpenSearch index for storing knowledge base documents.\n" +
			"Use --label to set the base's default knowledge label; sources ingested\n" +
			"without an explicit label inherit it. Without --label, the default follows\n" +
			"the naming convention ('upstream' for names containing \"upstream\", else\n" +
			"'canonical'). Define what labels mean to the LLM in your prompt variants.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			knowledgeBaseName := args[0]

			if labelFlag != "" {
				if err := knowledge.ValidateLabel(labelFlag); err != nil {
					return err
				}
			}

			if dc := daemonClient(cmd.Context); dc != nil {
				if _, err := dc.CreateKnowledge(context.Background(), knowledgeBaseName, labelFlag); err != nil {
					return err
				}
				fmt.Printf("Knowledge base '%s' created successfully.\n", knowledgeBaseName)
				return nil
			}

			indexName := knowledge.FullIndexName(knowledgeBaseName)

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			ctx := context.Background()
			if err := client.CreateIndex(ctx, indexName); err != nil {
				return fmt.Errorf("creating index: %w", err)
			}
			if labelFlag != "" {
				if err := client.SetDefaultLabel(ctx, indexName, labelFlag); err != nil {
					return fmt.Errorf("setting default label: %w", err)
				}
			}

			fmt.Printf("Knowledge base '%s' created successfully.\n", knowledgeBaseName)
			return nil
		},
	}

	cobraCmd.Flags().StringVarP(&labelFlag, "label", "l", "", "Default knowledge label for sources ingested into this base")

	return cobraCmd
}

func (cmd *knowledgeCommand) labelCommand() *cobra.Command {
	var applyToExisting bool

	cobraCmd := &cobra.Command{
		Use:   "label <knowledge_base_name> [<label>]",
		Short: "Show or set a knowledge base's default label",
		Long: "Show or set the default knowledge label of a knowledge base.\n" +
			"With no <label> argument, the effective default is printed together with\n" +
			"whether it is stored or derived from the naming convention.\n" +
			"Setting a label affects future ingests only, unless --apply-to-existing is\n" +
			"given, which also backfills chunks and sources that have no label yet\n" +
			"(explicit per-source labels are never overwritten).\n" +
			"Labels have no built-in meaning: reference them in your system prompt\n" +
			"variants ('prompt' command) to tell the LLM how to prioritize them.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			knowledgeBaseName := args[0]
			ctx := context.Background()

			// Show mode.
			if len(args) == 1 {
				if applyToExisting {
					return fmt.Errorf("--apply-to-existing requires a <label> argument")
				}
				if dc := daemonClient(cmd.Context); dc != nil {
					kb, err := dc.GetKnowledge(ctx, knowledgeBaseName)
					if err != nil {
						return err
					}
					fmt.Printf("Default label: %s\n", kb.DefaultLabel)
					return nil
				}
				client, err := cmd.opensearchClient()
				if err != nil {
					return err
				}
				indexName := knowledge.FullIndexName(knowledgeBaseName)
				label, stored, err := client.GetDefaultLabel(ctx, indexName)
				if err != nil {
					return err
				}
				origin := "derived from the base name (not stored)"
				if stored {
					origin = "stored"
				}
				fmt.Printf("Default label: %s (%s)\n", label, origin)
				return nil
			}

			// Set mode.
			label := args[1]
			if err := knowledge.ValidateLabel(label); err != nil {
				return err
			}

			if dc := daemonClient(cmd.Context); dc != nil {
				opURL, err := dc.SetKnowledgeLabel(ctx, knowledgeBaseName, label, applyToExisting)
				if err != nil {
					return err
				}
				if opURL != "" {
					if _, err := waitWithProgress(dc, opURL, "Backfilling labels", "", ""); err != nil {
						return err
					}
				}
				fmt.Printf("Default label of '%s' set to '%s'.\n", knowledgeBaseName, label)
				return nil
			}

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}
			indexName := knowledge.FullIndexName(knowledgeBaseName)
			if err := client.SetDefaultLabel(ctx, indexName, label); err != nil {
				return err
			}
			fmt.Printf("Default label of '%s' set to '%s'.\n", knowledgeBaseName, label)

			if applyToExisting {
				updated, err := client.BackfillLabel(ctx, indexName, label)
				if err != nil {
					return fmt.Errorf("backfilling labels: %w", err)
				}
				fmt.Printf("Labeled %d existing chunk(s) that had no label.\n", updated)
			}
			return nil
		},
	}

	cobraCmd.Flags().BoolVar(&applyToExisting, "apply-to-existing", false, "Also label already-ingested chunks and sources that have no label")

	return cobraCmd
}

func (cmd *knowledgeCommand) ingestCommand() *cobra.Command {
	var fileFlag string
	var urlFlag string
	var batchFlag string
	var formatFlag string
	var labelFlag string
	var forceFlag bool

	cobraCmd := &cobra.Command{
		Use:   "ingest [<knowledge_base_name> <source_id>]",
		Short: "Ingest a document into the knowledge base",
		Long: "Ingest a document into the knowledge base index with the given source ID.\n" +
			"Provide the document via --file (local path) or --url (remote URL).\n" +
			"Use --batch <config.yaml> to ingest multiple documents from a YAML file.\n" +
			"Use --format rfp to ingest a CSV of previous RFP question/answer pairs\n" +
			"(columns: question, answer, source), one chunk per row.",
		Args: cobra.RangeArgs(0, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			if labelFlag != "" {
				if err := knowledge.ValidateLabel(labelFlag); err != nil {
					return err
				}
				if batchFlag != "" {
					return fmt.Errorf("--label is not allowed with --batch; set per-job labels in the YAML file")
				}
			}

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

			// Daemon mode: hand the source to ragd, which crawls/extracts and
			// indexes server-side as an async operation. The file upload is
			// streamed over the socket; URL crawling happens on the daemon.
			if dc := daemonClient(cmd.Context); dc != nil {
				var opURL string
				var err error
				if urlFlag != "" {
					opURL, err = dc.IngestURL(context.Background(), knowledgeBaseName, sourceID, urlFlag, labelFlag)
				} else {
					opURL, err = dc.IngestFile(context.Background(), knowledgeBaseName, sourceID, fileFlag, labelFlag)
				}
				if err != nil {
					return err
				}
				if _, err := waitWithProgress(dc, opURL, "Ingesting source", "sources_done", "sources_total"); err != nil {
					return err
				}
				fmt.Printf("Ingested source '%s' into knowledge base '%s'\n", sourceID, knowledgeBaseName)
				return nil
			}

			if formatFlag != "" && formatFlag != "rfp" {
				return fmt.Errorf("unsupported format %q (supported: rfp)", formatFlag)
			}
			if formatFlag == "rfp" && urlFlag != "" {
				return fmt.Errorf("--format rfp requires --file, not --url")
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

			var result *processing.IngestResult
			if formatFlag == "rfp" {
				result, err = processing.IngestRFP(filePath, sourceID)
			} else {
				result, err = processing.Ingest(apiUrls[tika], filePath, sourceID)
			}
			if err != nil {
				return fmt.Errorf("ingesting document: %w", err)
			}

			client, err := knowledge.NewClient(apiUrls[opensearch])
			if err != nil {
				return err
			}

			ctx := context.Background()

			// Resolve the source's label: explicit > base default > convention.
			label := labelFlag
			if label == "" {
				if label, _, err = client.GetDefaultLabel(ctx, indexName); err != nil {
					return fmt.Errorf("resolving base default label: %w", err)
				}
			}
			// Older indexes lack the label keyword mapping; ensure it before the
			// first labeled write so dynamic mapping cannot type the field wrong.
			if err := client.EnsureLabelMapping(ctx, indexName); err != nil {
				return fmt.Errorf("ensuring label mapping: %w", err)
			}

			// Build source metadata with status=processing
			now := time.Now().UTC().Format(knowledge.DateFormat)
			chunkOverlap := processing.DefaultChunkOverlap
			if formatFlag == "rfp" {
				chunkOverlap = 0
			}
			meta := knowledge.SourceMetadata{
				SourceID:      sourceID,
				FileName:      filepath.Base(filePath),
				FilePath:      metadataPath,
				Checksum:      result.Checksum,
				IndexName:     indexName,
				ChunkCount:    len(result.Chunks),
				ChunkSize:     processing.DefaultChunkSize,
				ChunkOverlap:  chunkOverlap,
				ContentLength: result.ContentLength,
				Label:         label,
				Status:        knowledge.StatusProcessing,
				IngestedAt:    now,
				UpdatedAt:     now,
			}
			if formatFlag == "rfp" {
				meta.ContentType = "text/csv"
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
					Label:     label,
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
	cobraCmd.Flags().StringVar(&formatFlag, "format", "", "Input format: 'rfp' for a CSV of question,answer,source rows (default: auto-detect via Tika)")
	cobraCmd.Flags().StringVarP(&labelFlag, "label", "l", "", "Knowledge label for this source (default: the base's default label)")
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

			if dc := daemonClient(cmd.Context); dc != nil {
				searchBases := bases
				if len(searchBases) == 0 {
					defaultBase, _ := knowledge.KnowledgeBaseNameFromIndex(knowledge.DefaultIndexName())
					searchBases = []string{defaultBase}
				}
				hits, err := dc.Search(context.Background(), query, searchBases, k)
				if err != nil {
					return err
				}
				if len(hits) == 0 {
					fmt.Println("No results found.")
					return nil
				}
				for i, hit := range hits {
					fmt.Printf("\n--- Result %d (score: %.4f, base: %s) %s ---\n", i+1, hit.Score, hit.Base, knowledge.LabelTag(hit.Label))
					fmt.Printf("  Source: %s\n", hit.SourceID)
					fmt.Printf("  Date:   %s\n", hit.CreatedAt)
					content := hit.Content
					if len(content) > 200 {
						content = content[:200] + "..."
					}
					fmt.Printf("  %s\n", content)
				}
				fmt.Printf("\nTotal: %d results\n", len(hits))
				return nil
			}

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
				fmt.Printf("\n--- Result %d (score: %.4f, index: %s) %s ---\n", i+1, hit.Score, hit.Index, knowledge.LabelTag(hit.Label))
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

			if dc := daemonClient(cmd.Context); dc != nil {
				if err := dc.DeleteSource(context.Background(), knowledgeBaseName, sourceID); err != nil {
					return err
				}
				fmt.Printf("Forgot source '%s' from knowledge base '%s'\n", sourceID, knowledgeBaseName)
				return nil
			}

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
			knowledgeBaseName := args[0]
			sourceID := args[1]

			if dc := daemonClient(cmd.Context); dc != nil {
				src, err := dc.GetSource(context.Background(), knowledgeBaseName, sourceID)
				if err != nil {
					return err
				}
				printSourceMetadata(knowledgeBaseName, src)
				return nil
			}

			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

			meta, err := client.GetSourceMetadata(context.Background(), sourceID)
			if err != nil {
				return fmt.Errorf("source not found: %w", err)
			}

			knowledgeBaseName, _ = knowledge.KnowledgeBaseNameFromIndex(meta.IndexName)

			fmt.Printf("Source ID:      %s\n", meta.SourceID)
			fmt.Printf("Knowledge base: %s\n", knowledgeBaseName)
			fmt.Printf("Status:         %s\n", meta.Status)
			fmt.Printf("File name:      %s\n", meta.FileName)
			fmt.Printf("File path:      %s\n", meta.FilePath)
			fmt.Printf("Content type:   %s\n", meta.ContentType)
			fmt.Printf("Content length: %d bytes\n", meta.ContentLength)
			fmt.Printf("Label:          %s\n", knowledge.ResolveLabel(meta.IndexName, meta.Label))
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

			// Daemon mode: list sources and delete server-side. The confirmation
			// prompt stays client-side (the API has no interactive confirm).
			if dc := daemonClient(cmd.Context); dc != nil {
				ctx := context.Background()
				sources, err := dc.ListSources(ctx, knowledgeBaseName)
				if err != nil {
					return err
				}
				printDeletePreview(knowledgeBaseName, indexName, len(sources))
				for _, s := range sources {
					fmt.Printf("  %-50s %-12s %-8d %-20s\n", s.SourceID, s.Status, s.ChunkCount, s.IngestedAt)
				}
				if err := confirmDeletion(knowledgeBaseName, indexName); err != nil {
					return err
				}
				if err := dc.DeleteKnowledge(ctx, knowledgeBaseName); err != nil {
					return err
				}
				fmt.Printf("Deleted knowledge base '%s'.\n", knowledgeBaseName)
				return nil
			}

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
			if err := confirmDeletion(knowledgeBaseName, indexName); err != nil {
				return err
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

	fmt.Printf("%-50s %-30s %-16s %-12s %-8s %-20s\n", "SOURCE ID", "KNOWLEDGE BASE", "LABEL", "STATUS", "CHUNKS", "INGESTED AT")
	for _, s := range sources {
		knowledgeBaseName, _ := knowledge.KnowledgeBaseNameFromIndex(s.IndexName)
		fmt.Printf("%-50s %-30s %-16s %-12s %-8d %-20s\n",
			s.SourceID, knowledgeBaseName, knowledge.ResolveLabel(s.IndexName, s.Label), s.Status, s.ChunkCount, s.IngestedAt)
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

			// Export runs client-side even when the daemon is enabled: it writes
			// to the user's filesystem (default: ./<kb>-export), which the
			// strictly-confined daemon cannot reach (no home plug, and its own
			// working directory differs from the user's shell). Routing it to the
			// daemon would silently write the archive into an inaccessible
			// directory. The CLI has the home plug and OpenSearch access.
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
			"  --all                import all archives without interactive selection\n\n" +
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

			ctx := context.Background()

			// ── Local import ────────────────────────────────────────────────
			// Import runs client-side even when the daemon is enabled: it reads
			// the export directory/archive from the user's filesystem, which the
			// strictly-confined daemon cannot reach (no home plug). The CLI has
			// the home plug and OpenSearch access. The Google Drive flow below is
			// likewise CLI-only (interactive auth, per design).
			client, err := cmd.opensearchClient()
			if err != nil {
				return err
			}

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

			accessToken, err := knowledge.LoadOrAuthenticateDrive(ctx, cmd.Context.Config)
			if err != nil {
				return fmt.Errorf("Drive authentication: %w", err)
			}

			var archives []knowledge.DriveArchive

			switch kind {
			case knowledge.DriveKindFolder:
				stop := common.StartProgressSpinner("Listing archives in Google Drive folder")
				archives, err = knowledge.ListDriveArchives(ctx, resourceID, accessToken)
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
				// Fetch the actual filename so KB naming works correctly.
				stop := common.StartProgressSpinner("Fetching file metadata")
				fileName, metaErr := knowledge.GetDriveFileName(ctx, resourceID, accessToken)
				stop()
				if metaErr != nil || fileName == "" {
					fileName = resourceID + ".tar.gz"
				}
				archives = []knowledge.DriveArchive{{ID: resourceID, Name: fileName}}
			}

			for i, archive := range archives {
				fmt.Printf("[%d/%d] Downloading %s...\n", i+1, len(archives), archive.Name)
				tmpPath, cleanup, dlErr := knowledge.DownloadDriveArchive(ctx, archive, accessToken)
				if dlErr != nil {
					fmt.Printf("  skip: %v\n", dlErr)
					continue
				}

				// Derive a KB name from the archive filename when none is provided.
				target := kbName
				if target == "" {
					target = archiveStem(archive.Name)
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
