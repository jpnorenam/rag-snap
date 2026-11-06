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

	res := SearchWithKapa(context.Background(), nil, nil, "q", "q", "", 5, kapa, nil)
	if res.Err() != nil || res.KapaErr != nil || len(res.Hits) != 0 || calls != 0 || res.KapaQueried {
		t.Fatalf("no groups: %+v calls=%d, want kapa.ai skipped", res, calls)
	}

	res = SearchWithKapa(context.Background(), nil, nil, "q", "q", "", 5, kapa, []string{"g"})
	if res.Err() != nil || res.KapaErr != nil {
		t.Fatalf("err=%v kapaErr=%v", res.Err(), res.KapaErr)
	}
	// kapa.ai's own order is kept: its scores are rank-derived (1, 1/2, ...).
	if len(res.Hits) != 2 || res.Hits[0].Content != "one" || res.Hits[1].Content != "two" || res.Hits[0].Index != KapaIndexName {
		t.Errorf("hits = %+v, want kapa.ai hits in kapa.ai's order", res.Hits)
	}
	if res.Kapa != 2 || res.Local != 0 {
		t.Errorf("counts local=%d kapa=%d, want 0 and 2", res.Local, res.Kapa)
	}

	failing := NewKapaClientAt("http://127.0.0.1:1", "proj", "key")
	res = SearchWithKapa(context.Background(), nil, nil, "q", "q", "", 5, failing, []string{"g"})
	if res.Err() != nil || res.KapaErr == nil || len(res.Hits) != 0 {
		t.Errorf("failing kapa.ai: %+v, want only KapaErr set", res)
	}
}

// TestSearchWithKapaLocalFailure checks that a local failure keeps the kapa.ai
// hits as a warning, and fails the search only when kapa.ai gives nothing to
// fall back on.
func TestSearchWithKapaLocalFailure(t *testing.T) {
	// A client for an address nothing listens on, so every local search fails.
	t.Setenv(envOpenSearchUsername, "user")
	t.Setenv(envOpenSearchPassword, "pass")
	local, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	indexes := []string{"rag-default"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"source_url":"https://a","content":"one"}]`))
	}))
	t.Cleanup(srv.Close)
	kapa := NewKapaClientAt(srv.URL, "proj", "key")
	failing := NewKapaClientAt("http://127.0.0.1:1", "proj", "key")

	res := SearchWithKapa(context.Background(), local, indexes, "q", "q", "model", 5, kapa, []string{"g"})
	if res.LocalErr == nil || res.Err() != nil || len(res.Hits) != 1 {
		t.Errorf("kapa.ai ok: %+v, want kapa.ai hits kept and LocalErr only as a warning", res)
	}

	res = SearchWithKapa(context.Background(), local, indexes, "q", "q", "model", 5, nil, nil)
	if res.Err() == nil {
		t.Errorf("local only: Err() = nil, want the local failure")
	}

	res = SearchWithKapa(context.Background(), local, indexes, "q", "q", "model", 5, failing, []string{"g"})
	if res.Err() == nil || res.KapaErr == nil {
		t.Errorf("both failed: %+v, want Err() set", res)
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
