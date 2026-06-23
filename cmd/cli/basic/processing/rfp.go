package processing

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// IngestRFP parses a CSV file of previous RFP question/answer pairs
// (Column A: question, Column B: answer, Column C: source reference) and
// converts each row into a chunk that keeps the question and answer
// together, so retrieval on a similar future question returns the paired
// answer as a single unit. The first row is treated as a header and skipped.
func IngestRFP(filePath, sourceID string) (*IngestResult, error) {
	checksum, fileSize, err := checksumAndSize(filePath)
	if err != nil {
		return nil, fmt.Errorf("computing file checksum: %w", err)
	}
	if err := ValidateFileSize(fileSize); err != nil {
		return nil, err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // tolerate rows with a missing source column

	if _, err := reader.Read(); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("file %s is empty", filePath)
		}
		return nil, fmt.Errorf("reading header row: %w", err)
	}

	now := time.Now().UTC().Format(dateFormat)

	var chunks []Chunk
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}

		question := strings.TrimSpace(field(record, 0))
		answer := strings.TrimSpace(field(record, 1))
		source := strings.TrimSpace(field(record, 2))

		if question == "" && answer == "" {
			continue
		}

		chunks = append(chunks, rfpRowChunks(question, answer, source, sourceID, now)...)
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no question/answer rows found in %s", filePath)
	}

	return &IngestResult{
		Chunks:        chunks,
		Checksum:      checksum,
		ContentLength: fileSize,
	}, nil
}

// field returns record[i], or "" if the row doesn't have that many columns.
func field(record []string, i int) string {
	if i < len(record) {
		return record[i]
	}
	return ""
}

// rfpRowChunks builds the chunk(s) for a single Q&A row. The question is
// kept attached to the answer (and to every overflow segment, if the answer
// is too long to fit in one chunk) so the embedding always represents the
// question/answer pair rather than either half on its own.
func rfpRowChunks(question, answer, source, sourceID, createdAt string) []Chunk {
	prefix := fmt.Sprintf("Question: %s\n\nAnswer: ", question)
	suffix := ""
	if source != "" {
		suffix = fmt.Sprintf("\n\nReference: %s", source)
	}

	full := prefix + answer + suffix
	if len(full) <= DefaultChunkSize {
		return []Chunk{{Content: full, SourceID: sourceID, CreatedAt: createdAt}}
	}

	maxAnswerLen := DefaultChunkSize - len(prefix) - len(suffix)
	if maxAnswerLen < 1 {
		maxAnswerLen = DefaultChunkSize
	}

	segments := recursiveSplit(answer, maxAnswerLen)
	chunks := make([]Chunk, 0, len(segments))
	for _, seg := range segments {
		content := strings.TrimSpace(seg)
		if content == "" {
			continue
		}
		chunks = append(chunks, Chunk{
			Content:   prefix + content + suffix,
			SourceID:  sourceID,
			CreatedAt: createdAt,
		})
	}
	return chunks
}
