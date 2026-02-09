package processing

import (
	"strings"
	"time"
)

const (
	DefaultChunkSize    = 1024
	DefaultChunkOverlap = 200

	// dateFormat matches the OpenSearch index mapping format: "yyyy-MM-dd HH:mm:ss"
	dateFormat = "2006-01-02 15:04:05"
)

// Chunk represents a text segment ready for indexing into OpenSearch.
// Fields match the KNN index mapping defined in knowledge/indexes.go.
type Chunk struct {
	Content   string `json:"content"`
	SourceID  string `json:"source_id"`
	CreatedAt string `json:"created_at"`
}

// ChunkOptions configures the text chunking behavior.
type ChunkOptions struct {
	Size    int
	Overlap int
}

// ChunkText splits text into overlapping chunks with metadata.
// It tries to split at natural boundaries (paragraphs, lines, sentences, words)
// and adds overlap between consecutive chunks for context continuity.
func ChunkText(text, sourceID string, opts ChunkOptions) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	now := time.Now().UTC().Format(dateFormat)
	segments := recursiveSplit(text, opts.Size)

	var chunks []Chunk
	for i, seg := range segments {
		content := strings.TrimSpace(seg)
		if content == "" {
			continue
		}

		// Prepend overlap from the tail of the previous segment
		if i > 0 && opts.Overlap > 0 {
			tail := tailChars(segments[i-1], opts.Overlap)
			if tail != "" {
				content = strings.TrimSpace(tail) + " " + content
			}
		}

		chunks = append(chunks, Chunk{
			Content:   content,
			SourceID:  sourceID,
			CreatedAt: now,
		})
	}

	return chunks
}

// separators defines the hierarchy of split points tried in order:
// paragraph break → line break → sentence end → word boundary.
var separators = []string{"\n\n", "\n", ". ", " "}

// recursiveSplit splits text into segments no larger than maxSize,
// trying natural boundary separators before falling back to hard splits.
func recursiveSplit(text string, maxSize int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}

	for _, sep := range separators {
		parts := strings.SplitAfter(text, sep)
		if len(parts) > 1 && len(parts[0]) < len(text) {
			return mergeParts(parts, maxSize)
		}
	}

	// Last resort: hard character split
	var result []string
	for len(text) > maxSize {
		result = append(result, text[:maxSize])
		text = text[maxSize:]
	}
	if text != "" {
		result = append(result, text)
	}
	return result
}

// mergeParts combines small text parts into segments up to maxSize.
// Parts that exceed maxSize are recursively split further.
func mergeParts(parts []string, maxSize int) []string {
	var result []string
	var current strings.Builder

	for _, part := range parts {
		if current.Len()+len(part) > maxSize && current.Len() > 0 {
			result = append(result, current.String())
			current.Reset()
		}

		if len(part) > maxSize {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			sub := recursiveSplit(part, maxSize)
			result = append(result, sub...)
			continue
		}

		current.WriteString(part)
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// blockKind identifies whether a parsed block is prose or a table.
type blockKind int

const (
	blockText  blockKind = iota
	blockTable           // lines starting with |
)

// block represents a structural segment of Markdown content.
type block struct {
	kind    blockKind
	content string
	heading string // nearest preceding heading (for context on table chunks)
}

// ChunkMarkdown splits Markdown text into chunks with structure awareness.
// Tables are kept atomic when they fit in a single chunk; oversized tables
// are split with header repetition. Overlap is applied between consecutive
// prose chunks but skipped across table/prose boundaries.
func ChunkMarkdown(text, sourceID string, opts ChunkOptions) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	now := time.Now().UTC().Format(dateFormat)
	blocks := parseBlocks(text)
	segments := chunkBlocks(blocks, opts)

	var chunks []Chunk
	for _, seg := range segments {
		content := strings.TrimSpace(seg)
		if content == "" {
			continue
		}
		chunks = append(chunks, Chunk{
			Content:   content,
			SourceID:  sourceID,
			CreatedAt: now,
		})
	}

	return chunks
}

// parseBlocks splits Markdown text on double-newlines and classifies each
// segment as either a table block or a text block. It tracks the most recent
// heading to attach as context to table blocks.
func parseBlocks(text string) []block {
	paragraphs := strings.Split(text, "\n\n")
	var blocks []block
	var currentHeading string

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// Check if this paragraph is a heading
		if strings.HasPrefix(para, "#") {
			// Extract the heading line (first line if multi-line)
			lines := strings.SplitN(para, "\n", 2)
			currentHeading = strings.TrimSpace(lines[0])
			blocks = append(blocks, block{
				kind:    blockText,
				content: para,
			})
			continue
		}

		// Check if this is a table (lines starting with |)
		lines := strings.Split(para, "\n")
		isTable := true
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "|") {
				isTable = false
				break
			}
		}

		if isTable {
			blocks = append(blocks, block{
				kind:    blockTable,
				content: para,
				heading: currentHeading,
			})
		} else {
			blocks = append(blocks, block{
				kind:    blockText,
				content: para,
			})
		}
	}

	return blocks
}

// chunkBlocks processes blocks into string segments respecting structure.
// Text blocks are accumulated and flushed when they exceed the size limit.
// Table blocks are emitted atomically or split with header repetition.
func chunkBlocks(blocks []block, opts ChunkOptions) []string {
	var result []string
	var proseBuf strings.Builder
	var lastProseSegment string

	flushProse := func() {
		if proseBuf.Len() == 0 {
			return
		}
		text := proseBuf.String()
		proseBuf.Reset()

		// Split oversized prose using the existing recursive splitter
		segments := recursiveSplit(text, opts.Size)
		for i, seg := range segments {
			content := strings.TrimSpace(seg)
			if content == "" {
				continue
			}
			// Apply overlap from previous prose segment
			if opts.Overlap > 0 && (i > 0 || lastProseSegment != "") {
				var prev string
				if i > 0 {
					prev = segments[i-1]
				} else {
					prev = lastProseSegment
				}
				tail := tailChars(prev, opts.Overlap)
				if tail != "" {
					content = strings.TrimSpace(tail) + " " + content
				}
			}
			result = append(result, content)
		}
		lastProseSegment = segments[len(segments)-1]
	}

	for _, b := range blocks {
		switch b.kind {
		case blockText:
			// Check if adding this block would exceed the chunk size
			addition := b.content
			if proseBuf.Len() > 0 {
				addition = "\n\n" + addition
			}
			if proseBuf.Len()+len(addition) > opts.Size && proseBuf.Len() > 0 {
				flushProse()
			}
			if proseBuf.Len() > 0 {
				proseBuf.WriteString("\n\n")
			}
			proseBuf.WriteString(b.content)

		case blockTable:
			// If the prose buffer contains only the heading that the table
			// will carry as context, discard it to avoid duplication.
			if b.heading != "" && strings.TrimSpace(proseBuf.String()) == b.heading {
				proseBuf.Reset()
			} else {
				flushProse()
			}
			// No overlap between prose and table
			lastProseSegment = ""

			prefix := ""
			if b.heading != "" {
				prefix = b.heading + "\n\n"
			}
			tableContent := prefix + b.content

			if len(tableContent) <= opts.Size {
				result = append(result, tableContent)
			} else {
				parts := splitTable(b.content, b.heading, opts.Size)
				result = append(result, parts...)
			}
		}
	}

	// Flush remaining prose
	flushProse()

	return result
}

// splitTable splits a Markdown table into multiple chunks, repeating the
// header row and separator in each chunk for context.
func splitTable(tableText, heading string, maxSize int) []string {
	lines := strings.Split(tableText, "\n")
	if len(lines) < 2 {
		// Not enough lines to be a proper table
		content := tableText
		if heading != "" {
			content = heading + "\n\n" + content
		}
		return []string{content}
	}

	// Extract header (first row) and separator (second row)
	headerLine := lines[0]
	sepLine := lines[1]
	header := headerLine + "\n" + sepLine + "\n"

	prefix := ""
	if heading != "" {
		prefix = heading + "\n\n"
	}

	headerWithPrefix := prefix + header
	dataLines := lines[2:]

	var result []string
	var batch strings.Builder
	batch.WriteString(headerWithPrefix)

	for _, line := range dataLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lineWithNewline := line + "\n"
		if batch.Len()+len(lineWithNewline) > maxSize && batch.Len() > len(headerWithPrefix) {
			// Flush current batch
			result = append(result, strings.TrimRight(batch.String(), "\n"))
			batch.Reset()
			batch.WriteString(headerWithPrefix)
		}
		batch.WriteString(lineWithNewline)
	}

	if batch.Len() > len(headerWithPrefix) {
		result = append(result, strings.TrimRight(batch.String(), "\n"))
	}

	return result
}

// tailChars returns the last n characters of s, breaking at a word boundary.
func tailChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	sub := s[len(s)-n:]
	if idx := strings.Index(sub, " "); idx >= 0 {
		return sub[idx+1:]
	}
	return sub
}
