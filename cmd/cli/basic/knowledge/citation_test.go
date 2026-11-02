package knowledge

import (
	"context"
	"errors"
	"testing"
)

type fakeMetadataStore struct {
	metas map[string]SourceMetadata
	err   error
	calls int
	ids   []string
}

func (f *fakeMetadataStore) MultiGetSourceMetadata(_ context.Context, ids []string) (map[string]SourceMetadata, error) {
	f.calls++
	f.ids = ids
	if f.err != nil {
		return nil, f.err
	}
	return f.metas, nil
}

func resolveWith(store SourceMetadataStore, hits []SearchHit) []SearchHit {
	chain := []CitationResolver{KapaResolver{}, MetadataResolver{Store: store}, SourceIDResolver{}}
	ResolveCitations(context.Background(), chain, hits)
	return hits
}

func localHit(sourceID string) SearchHit {
	return SearchHit{Index: "rag-snap-context-docs", SourceID: sourceID, Label: LabelCanonical}
}

func TestCitationLinkAndLabel(t *testing.T) {
	cases := []struct {
		name      string
		meta      *SourceMetadata
		wantLabel string
		wantHref  string
	}{
		{
			name:      "URL-ingested source links to its file_path",
			meta:      &SourceMetadata{FilePath: "https://ubuntu.com/pro", FileName: "rag-crawl-123.html"},
			wantLabel: "src",
			wantHref:  "https://ubuntu.com/pro",
		},
		{
			name: "recorded source URI wins over file path",
			meta: &SourceMetadata{
				SourceURI: "https://github.com/canonical/k8s-snap/blob/main/README.md",
				FilePath:  "/home/me/README.md",
				FileName:  "README.md",
			},
			wantLabel: "README.md",
			wantHref:  "https://github.com/canonical/k8s-snap/blob/main/README.md",
		},
		{
			name:      "repo source ingested before this change gets no link",
			meta:      &SourceMetadata{FilePath: "docs/install.md", FileName: "rag-github-998877.md"},
			wantLabel: "src",
			wantHref:  "",
		},
		{
			name:      "usable title is the label",
			meta:      &SourceMetadata{Title: "Ubuntu Pro  Service\nDescription", FileName: "service-description.pdf"},
			wantLabel: "Ubuntu Pro Service Description",
		},
		{
			name:      "office placeholder title falls back to file name",
			meta:      &SourceMetadata{Title: "Microsoft Word - draft.docx", FileName: "service-description.pdf"},
			wantLabel: "service-description.pdf",
		},
		{
			name:      "title equal to the file name stem falls back to file name",
			meta:      &SourceMetadata{Title: "service-description", FileName: "service-description.pdf"},
			wantLabel: "service-description.pdf",
		},
		{
			name:      "empty title falls back to file name",
			meta:      &SourceMetadata{FileName: "service-description.pdf"},
			wantLabel: "service-description.pdf",
		},
		{
			name:      "temporary file name falls back to source id",
			meta:      &SourceMetadata{FileName: "ragd-upload-4242.pdf", FilePath: "/tmp/ragd-upload-4242.pdf"},
			wantLabel: "src",
		},
		{
			name:      "imported base without metadata falls back to source id",
			meta:      nil,
			wantLabel: "src",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeMetadataStore{metas: map[string]SourceMetadata{}}
			if tc.meta != nil {
				store.metas["src"] = *tc.meta
			}
			hits := resolveWith(store, []SearchHit{localHit("src")})
			cit := hits[0].Citation
			if cit.Label != tc.wantLabel {
				t.Errorf("label = %q, want %q", cit.Label, tc.wantLabel)
			}
			if cit.Href != tc.wantHref {
				t.Errorf("href = %q, want %q", cit.Href, tc.wantHref)
			}
			if cit.Key != "src" {
				t.Errorf("key = %q, want %q", cit.Key, "src")
			}
			if cit.Base != "docs" || cit.Tag != LabelCanonical {
				t.Errorf("base/tag = %q/%q, want docs/%s", cit.Base, cit.Tag, LabelCanonical)
			}
		})
	}
}

func TestCitationMetadataUnavailable(t *testing.T) {
	store := &fakeMetadataStore{err: errors.New("index_not_found")}
	hits := resolveWith(store, []SearchHit{localHit("rfp-2024")})
	cit := hits[0].Citation
	if cit.Label != "rfp-2024" || cit.Href != "" || cit.Key != "rfp-2024" {
		t.Fatalf("citation = %+v, want label/key rfp-2024 and no href", cit)
	}
}

func TestCitationSharedKeyAndSingleLookup(t *testing.T) {
	store := &fakeMetadataStore{metas: map[string]SourceMetadata{
		"guide.pdf": {FileName: "guide.pdf"},
		"faq.md":    {FileName: "faq.md"},
	}}
	hits := resolveWith(store, []SearchHit{localHit("guide.pdf"), localHit("faq.md"), localHit("guide.pdf")})

	if hits[0].Citation.Key != hits[2].Citation.Key {
		t.Errorf("chunks of one document have different keys: %q vs %q", hits[0].Citation.Key, hits[2].Citation.Key)
	}
	if hits[0].Citation.Key == hits[1].Citation.Key {
		t.Errorf("distinct documents share key %q", hits[0].Citation.Key)
	}
	if store.calls != 1 || len(store.ids) != 2 {
		t.Errorf("metadata lookups = %d with ids %v, want 1 lookup of 2 distinct ids", store.calls, store.ids)
	}
}

func TestCitationKapaHit(t *testing.T) {
	store := &fakeMetadataStore{}
	hits := resolveWith(store, []SearchHit{{
		Index:    KapaIndexName,
		SourceID: "https://documentation.ubuntu.com/server/",
		Label:    LabelKapa,
	}})
	cit := hits[0].Citation
	if cit.Href != "https://documentation.ubuntu.com/server/" || cit.Label != cit.Href {
		t.Errorf("kapa citation = %+v, want href and label set to the source URL", cit)
	}
	if cit.Base != "kapa" {
		t.Errorf("base = %q, want kapa", cit.Base)
	}
	if store.calls != 0 {
		t.Errorf("kapa hits must not trigger a metadata lookup (calls = %d)", store.calls)
	}
}

func TestIsHTTPURL(t *testing.T) {
	for s, want := range map[string]bool{
		"https://ubuntu.com/pro":   true,
		"http://example.com":       true,
		"file:///home/me/spec.pdf": false,
		"/tmp/x.pdf":               false,
		"docs/install.md":          false,
		"https://":                 false,
		"":                         false,
	} {
		if got := IsHTTPURL(s); got != want {
			t.Errorf("IsHTTPURL(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestUsableTitle(t *testing.T) {
	cases := []struct {
		title, file string
		want        bool
	}{
		{"Ubuntu Pro Service Description", "sd.pdf", true},
		{"", "sd.pdf", false},
		{"ab", "sd.pdf", false},
		{"Untitled document", "sd.pdf", false},
		{"Microsoft PowerPoint - deck.pptx", "deck.pptx", false},
		{"SD.PDF", "sd.pdf", false},
		{"sd", "sd.pdf", false},
	}
	for _, tc := range cases {
		if got := usableTitle(tc.title, tc.file); got != tc.want {
			t.Errorf("usableTitle(%q, %q) = %v, want %v", tc.title, tc.file, got, tc.want)
		}
	}
}

func TestOriginalFileName(t *testing.T) {
	for in, want := range map[string]string{
		"https://ubuntu.com/pro":                 "pro",
		"https://ubuntu.com/":                    "",
		"https://example.com/docs/My%20Spec.pdf": "My Spec.pdf",
		"/home/me/specs/guide.pdf":               "guide.pdf",
		"docs/install.md":                        "install.md",
		"/tmp/rag-github-123.md":                 "",
		"":                                       "",
	} {
		if got := OriginalFileName(in); got != want {
			t.Errorf("OriginalFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSourceLine(t *testing.T) {
	cases := []struct{ label, href, id, want string }{
		{"Ubuntu Pro Service Description", "https://ubuntu.com/pro", "pro-guide", "Ubuntu Pro Service Description — https://ubuntu.com/pro (pro-guide)"},
		{"faq.md", "", "faq", "faq.md (faq)"},
		{"rfp-2024", "", "rfp-2024", "rfp-2024"},
		{"", "", "rfp-2024", "rfp-2024"},
	}
	for _, tc := range cases {
		if got := SourceLine(tc.label, tc.href, tc.id); got != tc.want {
			t.Errorf("SourceLine(%q, %q, %q) = %q, want %q", tc.label, tc.href, tc.id, got, tc.want)
		}
	}
}
