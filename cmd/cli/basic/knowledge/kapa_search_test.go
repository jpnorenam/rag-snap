package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSearchWithKapaSkipsUnselectedSources checks each side runs only when it
// has both a client and something selected, and that a kapa.ai failure is
// returned separately instead of failing the search.
func TestSearchWithKapaSkipsUnselectedSources(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`[{"source_url":"https://a","content":"one"},{"source_url":"https://b","content":"two"}]`))
	}))
	t.Cleanup(srv.Close)
	kapa := NewKapaClientAt(srv.URL, "proj", "key")

	hits, kapaErr, err := SearchWithKapa(context.Background(), nil, nil, "q", "q", "", 5, kapa, nil)
	if err != nil || kapaErr != nil || len(hits) != 0 || calls != 0 {
		t.Fatalf("no groups: hits=%d calls=%d err=%v kapaErr=%v, want kapa.ai skipped", len(hits), calls, err, kapaErr)
	}

	hits, kapaErr, err = SearchWithKapa(context.Background(), nil, nil, "q", "q", "", 5, kapa, []string{"g"})
	if err != nil || kapaErr != nil {
		t.Fatalf("err=%v kapaErr=%v", err, kapaErr)
	}
	// kapa.ai's own order is kept: its scores are rank-derived (1, 1/2, ...).
	if len(hits) != 2 || hits[0].Content != "one" || hits[1].Content != "two" || hits[0].Index != KapaIndexName {
		t.Errorf("hits = %+v, want kapa.ai hits in kapa.ai's order", hits)
	}

	failing := NewKapaClientAt("http://127.0.0.1:1", "proj", "key")
	hits, kapaErr, err = SearchWithKapa(context.Background(), nil, nil, "q", "q", "", 5, failing, []string{"g"})
	if err != nil || kapaErr == nil || len(hits) != 0 {
		t.Errorf("failing kapa.ai: hits=%d err=%v kapaErr=%v, want only kapaErr set", len(hits), err, kapaErr)
	}
}

// TestKapaSearchCapsResults: kapa.ai may return more chunks than the requested
// limit, so the client caps them.
func TestKapaSearchCapsResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"source_url":"https://a","content":"1"},{"source_url":"https://b","content":"2"},{"source_url":"https://c","content":"3"}]`))
	}))
	t.Cleanup(srv.Close)
	hits, err := NewKapaClientAt(srv.URL, "proj", "key").Search(context.Background(), "q", 2, []string{"g"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[1].Content != "2" {
		t.Errorf("hits = %+v, want the first 2", hits)
	}
}
