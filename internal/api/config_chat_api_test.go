package api

import (
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// backendKeys is a complete backend config, so a failure can only come from
// the key under test.
func backendKeys(extra map[string]any) map[string]any {
	keys := map[string]any{
		"chat.http.host":      "127.0.0.1",
		"chat.http.port":      "8324",
		"knowledge.http.host": "127.0.0.1",
		"knowledge.http.port": "9200",
		"tika.http.host":      "127.0.0.1",
		"tika.http.port":      "9998",
	}
	for k, v := range extra {
		keys[k] = v
	}
	return keys
}

// An invalid chat.api fails daemon startup like a missing chat.http.host does.
func TestResolveBackendURLsRejectsInvalidChatAPI(t *testing.T) {
	ctx := &common.Context{Config: newMemConfig(backendKeys(map[string]any{"chat.api": "bedrock"}), nil)}
	_, err := ResolveBackendURLs(ctx)
	if err == nil || !strings.Contains(err.Error(), "chat.api") {
		t.Fatalf("ResolveBackendURLs = %v, want an error naming chat.api", err)
	}
}

func TestResolveBackendURLsAcceptsChatAPI(t *testing.T) {
	for _, api := range []string{"", "openai", "anthropic"} {
		ctx := &common.Context{Config: newMemConfig(backendKeys(map[string]any{"chat.api": api}), nil)}
		if _, err := ResolveBackendURLs(ctx); err != nil {
			t.Errorf("chat.api=%q: ResolveBackendURLs = %v, want success", api, err)
		}
	}
}
