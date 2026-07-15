package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestResolvePath verifies route resolution: real files serve as-is, per-route
// directories resolve to their index.html (bug: a reload on /knowledge/ used to
// fall through to the root chat page), and unknown paths hit the SPA index.
func TestResolvePath(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":           {Data: []byte("<html>root</html>")},
		"knowledge/index.html": {Data: []byte("<html>kb</html>")},
		"favicon.ico":          {Data: []byte("icon")},
	}
	cases := []struct{ in, want string }{
		{"index.html", "index.html"},   // exact file
		{"favicon.ico", "favicon.ico"}, // exact asset
		{"knowledge/", "/knowledge/"},  // route dir with trailing slash
		{"knowledge", "/knowledge/"},   // route dir without trailing slash
		{"search/", "/"},               // unknown route → SPA index
		{"some/deep/link", "/"},        // unknown deep link → SPA index
	}
	for _, tc := range cases {
		if got := resolvePath(assets, tc.in); got != tc.want {
			t.Errorf("resolvePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestHandlerServesIndex verifies the embedded index.html is served at the root.
func TestHandlerServesIndex(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<html") {
		t.Errorf("index response is not HTML: %q", rec.Body.String())
	}
}

// TestHandlerSPAFallback verifies an unknown path under the SPA falls back to
// index.html so client-side routing works on deep links/reloads.
func TestHandlerSPAFallback(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/client/route", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("deep-link status = %d, want 200 (SPA fallback)", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<html") {
		t.Errorf("SPA fallback did not serve HTML index: %q", rec.Body.String())
	}
}
