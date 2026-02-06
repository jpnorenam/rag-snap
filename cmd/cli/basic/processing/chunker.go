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
		if len(parts) > 1 {
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
