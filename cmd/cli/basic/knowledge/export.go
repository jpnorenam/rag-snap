package knowledge

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ExportManifest describes the contents of an exported knowledge base.
type ExportManifest struct {
	Version     string `json:"version"`
	KBName      string `json:"kb_name"`
	IndexName   string `json:"index_name"`
	ExportedAt  string `json:"exported_at"`
	SourceCount int    `json:"source_count"`
	ChunkCount  int    `json:"chunk_count"`
}

// ExportOptions configures a knowledge base export.
type ExportOptions struct {
	OutputDir string
	Compress  bool
}

// findElasticdump locates the elasticdump binary, preferring the snap-bundled copy.
// Also returns the directory that contains the node binary so it can be added to
// the subprocess PATH (elasticdump has a #!/usr/bin/env node shebang).
func findElasticdump() (bin, nodeDir string, err error) {
	snapDir := os.Getenv("SNAP")
	if snapDir != "" {
		candidate := filepath.Join(snapDir, "usr", "bin", "elasticdump")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, filepath.Join(snapDir, "usr", "bin"), nil
		}
	}
	bin, err = exec.LookPath("elasticdump")
	if err != nil {
		return "", "", err
	}
	if nodePath, lookErr := exec.LookPath("node"); lookErr == nil {
		nodeDir = filepath.Dir(nodePath)
	}
	return bin, nodeDir, nil
}

// runElasticdump executes elasticdump with the given args, streaming stdout/stderr live.
// nodeDir, when non-empty, is prepended to PATH so the elasticdump shebang can resolve node.
// NODE_TLS_REJECT_UNAUTHORIZED=0 disables TLS verification at the Node.js runtime level,
// matching the Go client's InsecureSkipVerify behaviour for self-signed OpenSearch certs.
func runElasticdump(ctx context.Context, bin, nodeDir string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	env := append(os.Environ(), "NODE_TLS_REJECT_UNAUTHORIZED=0")
	if nodeDir != "" {
		env = append(env, "PATH="+nodeDir+":"+os.Getenv("PATH"))
	}
	cmd.Env = env
	return cmd.Run()
}

// ExportKnowledgeBase exports a knowledge base index and its source metadata to a directory.
func ExportKnowledgeBase(ctx context.Context, client *OpenSearchClient, kbName string, opts ExportOptions) error {
	indexName := FullIndexName(kbName)

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = fmt.Sprintf("./%s-export", kbName)
	}

	// Fail early if index does not exist.
	exists, err := client.IndexExists(ctx, indexName)
	if err != nil {
		return fmt.Errorf("checking index: %w", err)
	}
	if !exists {
		return fmt.Errorf("index %q not found — run 'knowledge create %s' first", indexName, kbName)
	}

	// Gather source metadata to compute totals for the manifest.
	sources, err := client.ListSourceMetadata(ctx, indexName)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}
	chunkCount := 0
	for _, s := range sources {
		chunkCount += s.ChunkCount
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	bin, nodeDir, err := findElasticdump()
	if err != nil {
		return fmt.Errorf("elasticdump not found: %w", err)
	}

	inputURL := client.AuthenticatedURL("/" + indexName)

	// Export document data.
	dataPath := filepath.Join(outputDir, "data.json")
	fmt.Printf("Exporting document data to %s...\n", dataPath)
	if err := runElasticdump(ctx, bin, nodeDir, []string{
		"--input=" + inputURL,
		"--output=" + dataPath,
		"--type=data",
		"--limit=100",
		"--tlsVerification=false",
	}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("exporting data: %w", err)
	}

	// Export index mapping.
	mappingPath := filepath.Join(outputDir, "mapping.json")
	fmt.Printf("Exporting mapping to %s...\n", mappingPath)
	if err := runElasticdump(ctx, bin, nodeDir, []string{
		"--input=" + inputURL,
		"--output=" + mappingPath,
		"--type=mapping",
		"--tlsVerification=false",
	}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("exporting mapping: %w", err)
	}

	// Export source metadata filtered to this index.
	sourcesPath := filepath.Join(outputDir, "sources.json")
	fmt.Printf("Exporting source metadata to %s...\n", sourcesPath)
	metaInputURL := client.AuthenticatedURL("/" + sourcesIndexName)
	searchBody := fmt.Sprintf(`{"query":{"term":{"index_name":"%s"}}}`, indexName)
	if err := runElasticdump(ctx, bin, nodeDir, []string{
		"--input=" + metaInputURL,
		"--output=" + sourcesPath,
		"--type=data",
		"--limit=100",
		"--tlsVerification=false",
		"--searchBody=" + searchBody,
	}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("exporting source metadata: %w", err)
	}

	// Write manifest last — a missing manifest signals an incomplete export.
	manifest := ExportManifest{
		Version:     "1",
		KBName:      kbName,
		IndexName:   indexName,
		ExportedAt:  time.Now().UTC().Format(DateFormat),
		SourceCount: len(sources),
		ChunkCount:  chunkCount,
	}
	manifestPath := filepath.Join(outputDir, "manifest.json")
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	if opts.Compress {
		tarPath := outputDir + ".tar.gz"
		fmt.Printf("Compressing to %s...\n", tarPath)
		if err := compressDir(outputDir, tarPath); err != nil {
			return fmt.Errorf("compressing export: %w", err)
		}
		if err := os.RemoveAll(outputDir); err != nil {
			return fmt.Errorf("removing temporary export directory: %w", err)
		}
		fmt.Printf("\nExport complete.\n")
		fmt.Printf("  Sources:  %d\n", len(sources))
		fmt.Printf("  Chunks:   %d\n", chunkCount)
		fmt.Printf("  Location: %s\n", tarPath)
		return nil
	}

	fmt.Printf("\nExport complete.\n")
	fmt.Printf("  Sources:  %d\n", len(sources))
	fmt.Printf("  Chunks:   %d\n", chunkCount)
	fmt.Printf("  Location: %s\n", outputDir)
	return nil
}

// compressDir creates a gzip-compressed tar archive of srcDir at destPath.
// Files are stored with paths relative to srcDir's parent so that extracting
// the archive recreates the original directory name.
func compressDir(srcDir, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating archive file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	base := filepath.Dir(srcDir)
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		_, err = io.Copy(tw, src)
		return err
	})
}
