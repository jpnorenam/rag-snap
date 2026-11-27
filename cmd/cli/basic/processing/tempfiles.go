package processing

import (
	"regexp"
	"strings"
)

// Prefixes for the temporary files ingestion stages content in. They are
// shared constants so citation resolution can recognize a recorded file name
// that is really a staging artifact (see IsTempFileName) — a new prefix added
// here is picked up there automatically.
const (
	TempPrefixCrawl  = "rag-crawl-"
	TempPrefixRepo   = "rag-github-"
	TempPrefixUpload = "ragd-upload-"
)

var tempFilePrefixes = []string{TempPrefixCrawl, TempPrefixRepo, TempPrefixUpload}

// os.CreateTemp replaces "*" with a run of decimal digits.
var tempSuffixPattern = regexp.MustCompile(`^\d+`)

// IsTempFileName reports whether name is the base name of a staging file
// created with one of the ingestion temp prefixes.
func IsTempFileName(name string) bool {
	for _, prefix := range tempFilePrefixes {
		if rest, ok := strings.CutPrefix(name, prefix); ok && tempSuffixPattern.MatchString(rest) {
			return true
		}
	}
	return false
}
