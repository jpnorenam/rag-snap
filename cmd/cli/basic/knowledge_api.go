package basic

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jpnorenam/rag-snap/internal/apiclient"
)

// listIndexesAPI lists knowledge bases via the daemon, matching the direct-mode
// listIndexes output.
func (cmd *knowledgeCommand) listIndexesAPI(ctx context.Context, dc *apiclient.Client) error {
	bases, err := dc.ListKnowledge(ctx)
	if err != nil {
		return err
	}
	if len(bases) == 0 {
		fmt.Println("No knowledge base indexes found.")
		return nil
	}
	fmt.Printf("%-30s %-10s %-10s %-12s %-10s\n", "KNOWLEDGE BASE", "HEALTH", "STATUS", "DOCS", "SIZE")
	for _, b := range bases {
		fmt.Printf("%-30s %-10s %-10s %-12s %-10s\n", b.Name, b.Health, b.Status, b.DocsCount, b.StoreSize)
	}
	return nil
}

// listSourcesAPI lists sources via the daemon, matching the direct-mode
// listSources output. An optional index filter selects a single base.
func (cmd *knowledgeCommand) listSourcesAPI(ctx context.Context, dc *apiclient.Client, args []string) error {
	var bases []apiclient.KnowledgeBase
	if len(args) > 0 {
		bases = []apiclient.KnowledgeBase{{Name: args[0]}}
	} else {
		var err error
		bases, err = dc.ListKnowledge(ctx)
		if err != nil {
			return err
		}
	}

	fmt.Printf("%-50s %-30s %-16s %-12s %-8s %-20s\n", "SOURCE ID", "KNOWLEDGE BASE", "LABEL", "STATUS", "CHUNKS", "INGESTED AT")
	found := false
	for _, b := range bases {
		sources, err := dc.ListSources(ctx, b.Name)
		if err != nil {
			return err
		}
		for _, s := range sources {
			found = true
			fmt.Printf("%-50s %-30s %-16s %-12s %-8d %-20s\n", s.SourceID, b.Name, s.Label, s.Status, s.ChunkCount, s.IngestedAt)
		}
	}
	if !found {
		fmt.Println("No ingested sources found.")
	}
	return nil
}

// printSourceMetadata renders a single source's metadata, matching the
// direct-mode metadata command output.
func printSourceMetadata(knowledgeBaseName string, meta *apiclient.Source) {
	fmt.Printf("Source ID:      %s\n", meta.SourceID)
	fmt.Printf("Knowledge base: %s\n", knowledgeBaseName)
	fmt.Printf("Status:         %s\n", meta.Status)
	fmt.Printf("File name:      %s\n", meta.FileName)
	fmt.Printf("File path:      %s\n", meta.FilePath)
	fmt.Printf("Content type:   %s\n", meta.ContentType)
	fmt.Printf("Content length: %d bytes\n", meta.ContentLength)
	fmt.Printf("Label:          %s\n", meta.Label)
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
}

// printDeletePreview prints the header shown before a knowledge base deletion.
func printDeletePreview(knowledgeBaseName, indexName string, sourceCount int) {
	if sourceCount == 0 {
		fmt.Printf("Knowledge base '%s' has no ingested sources.\n", knowledgeBaseName)
		return
	}
	fmt.Printf("The following %d source(s) will be permanently deleted:\n\n", sourceCount)
	fmt.Printf("  %-50s %-12s %-8s %-20s\n", "SOURCE ID", "STATUS", "CHUNKS", "INGESTED AT")
}

// confirmDeletion prompts the operator to type the knowledge base name to
// confirm a destructive delete, returning an error if it does not match.
func confirmDeletion(knowledgeBaseName, indexName string) error {
	fmt.Printf("\nThis will permanently delete the knowledge base '%s' and all its data.\n", knowledgeBaseName)
	fmt.Printf("Type the knowledge base name to confirm: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	if strings.TrimSpace(input) != knowledgeBaseName {
		return fmt.Errorf("confirmation does not match — deletion aborted")
	}
	return nil
}
