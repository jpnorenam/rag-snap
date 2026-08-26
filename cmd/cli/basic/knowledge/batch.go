package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"gopkg.in/yaml.v3"
)

// BatchJob describes a single document ingestion task within a batch config.
type BatchJob struct {
	Name       string   `yaml:"name,omitempty"`
	Type       string   `yaml:"type"`
	Source     string   `yaml:"source"`
	TargetKB   string   `yaml:"target_kb,omitempty"`
	Branch     string   `yaml:"branch,omitempty"`
	Extensions []string `yaml:"extensions,omitempty"`
	Path       string   `yaml:"path,omitempty"`
	Label      string   `yaml:"label,omitempty"`
}

// BatchConfig is the top-level structure of a batch YAML file.
type BatchConfig struct {
	Version string     `yaml:"version"`
	Jobs    []BatchJob `yaml:"jobs"`
}

// ProcessBatch reads a YAML batch file and ingests each job into OpenSearch.
// When force is false, sources that are already ingested (status=completed) are skipped.
func ProcessBatch(ctx context.Context, client *OpenSearchClient, tikaURL string, yamlPath string, force bool) error {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("reading batch file: %w", err)
	}

	var batchCfg BatchConfig
	if err := yaml.Unmarshal(data, &batchCfg); err != nil {
		return fmt.Errorf("parsing batch yaml: %w", err)
	}
	if len(batchCfg.Jobs) == 0 {
		return fmt.Errorf("batch file contains no jobs")
	}
	for i, job := range batchCfg.Jobs {
		if job.Label == "" {
			continue
		}
		if err := ValidateLabel(job.Label); err != nil {
			return fmt.Errorf("job %d (%s): %w", i+1, job.Source, err)
		}
	}
	if err := checkBatchTargets(ctx, client, batchCfg.Jobs); err != nil {
		return err
	}

	fmt.Printf("Found %d jobs in batch file version %s\n", len(batchCfg.Jobs), batchCfg.Version)

	for i, job := range batchCfg.Jobs {
		fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(batchCfg.Jobs), job.Source)

		if err := processSingleJob(ctx, client, tikaURL, job, force); err != nil {
			fmt.Printf("❌ Error processing %s: %v\n", job.Source, err)
			continue
		}
		fmt.Printf("✅ Success: %s\n", job.Source)
	}

	return nil
}

// checkBatchTargets verifies every job's target knowledge base exists before any
// job runs. Without this, a missing base surfaces as a per-file 404 from the
// first mapping read inside the ingest core, once for every file in the batch.
func checkBatchTargets(ctx context.Context, client *OpenSearchClient, jobs []BatchJob) error {
	checked := make(map[string]bool, len(jobs))
	var missing []string
	for _, job := range jobs {
		index := jobTargetIndex(job)
		if checked[index] {
			continue
		}
		checked[index] = true

		exists, err := client.IndexExists(ctx, index)
		if err != nil {
			return fmt.Errorf("checking knowledge base %q: %w", index, err)
		}
		if !exists {
			name, nameErr := KnowledgeBaseNameFromIndex(index)
			if nameErr != nil {
				name = index
			}
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("knowledge base(s) not found: %s\nCreate them first, e.g. 'rag-cli.rag k create %s'",
		strings.Join(missing, ", "), missing[0])
}

// jobTargetIndex returns the full index name a job ingests into.
func jobTargetIndex(job BatchJob) string {
	if job.TargetKB == "" {
		return DefaultIndexName()
	}
	return FullIndexName(job.TargetKB)
}

// processSingleJob ingests one job from a batch config into OpenSearch.
func processSingleJob(ctx context.Context, client *OpenSearchClient, tikaURL string, job BatchJob, force bool) error {
	targetIndex := jobTargetIndex(job)

	switch job.Type {
	case "file":
		path, err := filepath.Abs(job.Source)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
		sourceID := job.Name
		if sourceID == "" {
			sourceID = filepath.Base(path)
		}
		return ingestAndIndex(ctx, client, tikaURL, path, sourceID, targetIndex, job.Label, force)

	case "url":
		crawled, _, cleanup, err := processing.CrawlURL(job.Source)
		if err != nil {
			return fmt.Errorf("crawling URL: %w", err)
		}
		defer cleanup()
		sourceID := job.Name
		if sourceID == "" {
			sourceID = job.Source
		}
		return ingestAndIndex(ctx, client, tikaURL, crawled, sourceID, targetIndex, job.Label, force)

	case "github-repo":
		return processGitHubRepoJob(ctx, client, tikaURL, job, targetIndex, force)

	case "gitea-repo":
		return processGiteaRepoJob(ctx, client, tikaURL, job, targetIndex, force)

	default:
		return fmt.Errorf("unsupported job type %q (supported: file, url, github-repo, gitea-repo)", job.Type)
	}
}

// processGitHubRepoJob fetches all matching files from a GitHub repository and indexes them.
func processGitHubRepoJob(ctx context.Context, client *OpenSearchClient, tikaURL string, job BatchJob, targetIndex string, force bool) error {
	owner, repo, err := processing.ParseGitHubSource(job.Source)
	if err != nil {
		return fmt.Errorf("parsing GitHub source: %w", err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	entries, truncated, err := processing.ListGitHubRepoFiles(owner, repo, job.Branch, job.Path, job.Extensions, token)
	if err != nil {
		return fmt.Errorf("listing repository files: %w", err)
	}
	if truncated {
		fmt.Println("Warning: repository tree is truncated (>100k files); some files may be skipped")
	}

	fmt.Printf("Found %d files in %s/%s\n", len(entries), owner, repo)

	for i, entry := range entries {
		fmt.Printf("  [%d/%d] %s\n", i+1, len(entries), entry.Path)
		tempPath, cleanup, err := processing.FetchRepoFile(entry.RawURL, entry.Path, token)
		if err != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, err)
			continue
		}
		if ingestErr := ingestAndIndex(ctx, client, tikaURL, tempPath, entry.SourceID, targetIndex, job.Label, force); ingestErr != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, ingestErr)
		}
		cleanup()
	}
	return nil
}

// processGiteaRepoJob fetches all matching files from a Gitea repository and indexes them.
func processGiteaRepoJob(ctx context.Context, client *OpenSearchClient, tikaURL string, job BatchJob, targetIndex string, force bool) error {
	baseURL, owner, repo, err := processing.ParseGiteaSource(job.Source)
	if err != nil {
		return fmt.Errorf("parsing Gitea source: %w", err)
	}

	token := os.Getenv("GITEA_TOKEN")
	entries, truncated, err := processing.ListGiteaRepoFiles(baseURL, owner, repo, job.Branch, job.Path, job.Extensions, token)
	if err != nil {
		return fmt.Errorf("listing repository files: %w", err)
	}
	if truncated {
		fmt.Println("Warning: repository tree is truncated; some files may be skipped")
	}

	fmt.Printf("Found %d files in %s/%s\n", len(entries), owner, repo)

	for i, entry := range entries {
		fmt.Printf("  [%d/%d] %s\n", i+1, len(entries), entry.Path)
		tempPath, cleanup, err := processing.FetchRepoFile(entry.RawURL, entry.Path, token)
		if err != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, err)
			continue
		}
		if ingestErr := ingestAndIndex(ctx, client, tikaURL, tempPath, entry.SourceID, targetIndex, job.Label, force); ingestErr != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, ingestErr)
		}
		cleanup()
	}
	return nil
}

// ingestAndIndex is the CLI-side wrapper over the shared IngestSource core. When
// force is false, sources already marked as completed are skipped (batch policy);
// when force is set, IngestSource replaces the existing source's chunks.
func ingestAndIndex(ctx context.Context, client *OpenSearchClient, tikaURL, filePath, sourceID, targetIndex, label string, force bool) error {
	if !force && client.SourceCompleted(ctx, sourceID) {
		fmt.Printf("  already ingested, skipping: %s\n", sourceID)
		return nil
	}
	return client.IngestSource(ctx, tikaURL, IngestOptions{
		FilePath:    filePath,
		SourceID:    sourceID,
		TargetIndex: targetIndex,
		Label:       label,
		Force:       force,
	})
}
