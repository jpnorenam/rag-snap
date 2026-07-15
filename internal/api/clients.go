package api

import (
	"context"
	"fmt"
	"sync"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/cmd/cli/config"
)

// clientCache lazily builds and caches the long-lived backend clients the
// daemon owns. Clients are built on first use (so the daemon starts even when a
// backend is down) and reused thereafter. A reload (SIGHUP) constructs a new
// Server with a fresh cache, so no explicit invalidation is needed here.
type clientCache struct {
	urls map[string]string
	ctx  *common.Context

	mu         sync.Mutex
	openSearch *knowledge.OpenSearchClient
}

func newClientCache(ctx *common.Context, urls map[string]string) *clientCache {
	return &clientCache{ctx: ctx, urls: urls}
}

// openSearchClient returns the cached OpenSearchClient, building it on first
// use. The OpenSearch backend secrets come from the environment
// (OPENSEARCH_USERNAME/PASSWORD), read inside knowledge.NewClient. A build
// failure is not cached: a transient backend outage should not permanently
// fail the daemon, so the next request retries.
func (c *clientCache) openSearchClient() (*knowledge.OpenSearchClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.openSearch != nil {
		return c.openSearch, nil
	}
	url := c.urls[backendOpenSearch]
	if url == "" {
		return nil, fmt.Errorf("OpenSearch backend URL is not configured")
	}
	client, err := knowledge.NewClient(url)
	if err != nil {
		return nil, fmt.Errorf("knowledge backend unavailable: %w", err)
	}
	c.openSearch = client
	return client, nil
}

// openSearchClientNoWait is openSearchClient for callers that must not block: it
// builds the client with a single ctx-bounded readiness check instead of
// knowledge.NewClient's wait-for-ready loop, which retries for a minute against a
// down server. The status probe needs to report "unreachable" in seconds, not stall
// the request until the loop gives up. A client built either way is cached and shared.
func (c *clientCache) openSearchClientNoWait(ctx context.Context) (*knowledge.OpenSearchClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.openSearch != nil {
		return c.openSearch, nil
	}
	url := c.urls[backendOpenSearch]
	if url == "" {
		return nil, fmt.Errorf("OpenSearch backend URL is not configured")
	}
	client, err := knowledge.NewClientNoWait(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("knowledge backend unavailable: %w", err)
	}
	c.openSearch = client
	return client, nil
}

// embeddingModelID returns the configured embedding model ID, required for
// search and ingest.
func (c *clientCache) embeddingModelID() (string, error) {
	id, _ := config.GetString(c.ctx.Config, knowledge.ConfEmbeddingModelID)
	if id == "" {
		return "", fmt.Errorf("embedding model is not configured (set %s); retrieval is unavailable", knowledge.ConfEmbeddingModelID)
	}
	return id, nil
}

// confChatModel is the config key for the default chat model name. It mirrors
// the CLI's chatCommand resolution; an empty value means "ask the inference
// server for its model".
const confChatModel = "chat.model"

// chatModelID returns the configured default chat model, or "" when unset (the
// caller then looks the model up from the inference server).
func (c *clientCache) chatModelID() string {
	id, _ := config.GetString(c.ctx.Config, confChatModel)
	return id
}

// tikaURL returns the configured Tika base URL.
func (c *clientCache) tikaURL() string { return c.urls[backendTika] }

// openAIURL returns the configured inference server base URL.
func (c *clientCache) openAIURL() string { return c.urls[backendOpenAI] }
