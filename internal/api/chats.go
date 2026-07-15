package api

import (
	"context"
	"os"
	"path/filepath"

	"github.com/canonical/go-snapctl/env"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

// chatsRelDir is the saved-chat store location under $SNAP_COMMON, alongside the
// prompt store. $SNAP_COMMON (not $SNAP_DATA) so saved chats survive a snap
// refresh and are not reverted with a revision rollback.
const chatsRelDir = "ragd/chats"

// newChatStore resolves the store directory under $SNAP_COMMON (temp-dir fallback
// off-snap, as for the prompt store and socket). The directory itself is created
// lazily on the first save.
func newChatStore() *chatstore.Store {
	base := env.SnapCommon()
	if base == "" {
		// Outside a snap (local dev / tests), fall back to a temp dir.
		base = os.TempDir()
	}
	return chatstore.New(filepath.Join(base, chatsRelDir))
}

// filterExistingBases splits want into the base names that still exist as
// knowledge indexes and those that have since been deleted. A nil knowledge
// client (retrieval unavailable) drops every base, since none can be resolved.
// Order within each group follows want.
func filterExistingBases(ctx context.Context, kc *knowledge.OpenSearchClient, want []string) (kept, dropped []string) {
	if len(want) == 0 {
		return nil, nil
	}

	existing := map[string]bool{}
	if kc != nil {
		if idxs, err := kc.ListIndexes(ctx); err == nil {
			for _, idx := range idxs {
				if name, err := knowledge.KnowledgeBaseNameFromIndex(idx.Name); err == nil {
					existing[name] = true
				}
			}
		}
	}

	for _, b := range want {
		if existing[b] {
			kept = append(kept, b)
		} else {
			dropped = append(dropped, b)
		}
	}
	return kept, dropped
}
