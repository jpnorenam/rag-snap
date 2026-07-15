package webui

import (
	"io/fs"
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

// TestHandlerServesExportedRoutePage verifies a route with its own exported page
// is served that page, not the root index. The static export writes one document
// per route (status/index.html, prompts/index.html); serving the root index for
// them instead makes every deep link and reload render the *chat* page, whatever
// route the user actually asked for.
func TestHandlerServesExportedRoutePage(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	assets, err := Assets()
	if err != nil {
		t.Fatalf("Assets: %v", err)
	}

	// The embed contains a committed placeholder index.html even before the UI is
	// built, so only assert this where a real exported route page is present.
	if !hasIndex(assets, "status") {
		t.Skip("no exported status/ page in the embedded dist (run `make ui`)")
	}

	want, err := fs.ReadFile(assets, "status/index.html")
	if err != nil {
		t.Fatalf("reading status/index.html: %v", err)
	}
	root, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /status/ status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != string(want) {
		if got == string(root) {
			t.Fatal("GET /status/ served the root index (the chat page), not the status page")
		}
		t.Fatal("GET /status/ did not serve status/index.html")
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
