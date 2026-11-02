package processing

import "testing"

func TestMetaPagesReadsTikaPDFSpelling(t *testing.T) {
	// The exact key Tika 3.1.0 reports for a PDF. The XMP paged-text namespace is
	// "xmpTPg" with one P; the plausible-looking "xmpTPPg" matches nothing, and a
	// silently-zero page count means the page limit stops being enforced without
	// anything failing. Hence a test on the spelling rather than on the mechanism.
	if got := metaPages(map[string]any{"xmpTPg:NPages": "16"}); got != 16 {
		t.Errorf("metaPages = %d, want 16", got)
	}
}

func TestMetaPagesAcceptsNumbersAndAlternativeKeys(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want int
	}{
		{"numeric value", map[string]any{"xmpTPg:NPages": float64(7)}, 7},
		{"office page count", map[string]any{"Page-Count": "3"}, 3},
		{"meta page count", map[string]any{"meta:page-count": "5"}, 5},
		{"per-page array length", map[string]any{"pdf:charsPerPage": []any{"1", "2", "3", "4"}}, 4},
		{"explicit count wins over array", map[string]any{
			"xmpTPg:NPages":    "9",
			"pdf:charsPerPage": []any{"1", "2"},
		}, 9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := metaPages(tc.meta); got != tc.want {
				t.Errorf("metaPages = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMetaPagesReturnsZeroWhenAbsentOrUnusable(t *testing.T) {
	// 0 means "unknown", which callers must treat as "not checked" rather than as a
	// document with no pages.
	tests := []struct {
		name string
		meta map[string]any
	}{
		{"empty", map[string]any{}},
		{"unrelated keys", map[string]any{"Content-Type": "application/pdf"}},
		{"non-numeric", map[string]any{"xmpTPg:NPages": "many"}},
		{"zero", map[string]any{"xmpTPg:NPages": "0"}},
		{"negative", map[string]any{"xmpTPg:NPages": float64(-2)}},
		{"empty array", map[string]any{"pdf:charsPerPage": []any{}}},
		{"wrong type", map[string]any{"xmpTPg:NPages": true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := metaPages(tc.meta); got != 0 {
				t.Errorf("metaPages = %d, want 0", got)
			}
		})
	}
}
