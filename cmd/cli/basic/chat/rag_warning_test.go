package chat

import (
	"errors"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// TestRetrievalWarning checks the warning names only fallbacks that exist.
func TestRetrievalWarning(t *testing.T) {
	kapaDown, localDown := errors.New("kapa down"), errors.New("opensearch down")
	tests := []struct {
		name string
		res  knowledge.CombinedSearch
		want string // substring; "" means no warning
	}{
		{"all ok", knowledge.CombinedSearch{LocalQueried: true, KapaQueried: true}, ""},
		{"kapa failed, local ok", knowledge.CombinedSearch{LocalQueried: true, KapaQueried: true, KapaErr: kapaDown},
			"answering from local knowledge only"},
		{"kapa failed, no local bases", knowledge.CombinedSearch{KapaQueried: true, KapaErr: kapaDown},
			"no local knowledge bases are active"},
		{"both failed", knowledge.CombinedSearch{LocalQueried: true, KapaQueried: true, KapaErr: kapaDown, LocalErr: localDown},
			"local retrieval also failed (opensearch down)"},
		{"local failed, kapa ok", knowledge.CombinedSearch{LocalQueried: true, KapaQueried: true, LocalErr: localDown},
			"answering from kapa.ai only"},
		{"local failed, no kapa", knowledge.CombinedSearch{LocalQueried: true, LocalErr: localDown},
			"no retrieved context"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retrievalWarning(tt.res)
			if tt.want == "" {
				if got != "" {
					t.Errorf("got %q, want no warning", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want it to contain %q", got, tt.want)
			}
			if tt.res.KapaErr != nil && tt.res.LocalErr == nil && !tt.res.LocalQueried && strings.Contains(got, "local knowledge only") {
				t.Errorf("got %q: claims a local fallback that does not exist", got)
			}
		})
	}
}
