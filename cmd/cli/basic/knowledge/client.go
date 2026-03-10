package knowledge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	opensearch "github.com/opensearch-project/opensearch-go/v4"
	opensearchapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

const (
	envOpenSearchUsername = "OPENSEARCH_USERNAME"
	envOpenSearchPassword = "OPENSEARCH_PASSWORD"

	ConfEmbeddingModelID = "knowledge.model.embedding"
	ConfRerankModelID    = "knowledge.model.rerank"
)

type OpenSearchClient struct {
	client           *opensearchapi.Client
	url              string
	username         string
	password         string
	embeddingModelID string
	ingestPipeline   string
	rerankModelID    string
	searchPipeline   string
}

// URL returns the OpenSearch server URL.
func (c *OpenSearchClient) URL() string {
	return c.url
}

// headerTransport wraps an http.RoundTripper and adds default headers to all requests.
type headerTransport struct {
	transport http.RoundTripper
}

// InitPipelines initializes OpenSearch pipelines, models, indexes, and templates.
// InitPipelines initializes OpenSearch pipelines, models, indexes, and templates.
func (c *OpenSearchClient) InitPipelines(ctx context.Context) error {
	if err := c.Init(ctx); err != nil {
		return fmt.Errorf("error initializing OpenSearch client: %w", err)
	}
	return nil
}

// ListIndexes retrieves all indexes matching the knowledge base pattern.
func (c *OpenSearchClient) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	return c.catIndexes(ctx)
}

// CreateIndex ensures the named index exists.
func (c *OpenSearchClient) CreateIndex(ctx context.Context, indexName string) error {
	return c.getOrCreateIndex(ctx, indexName)
}

// NewClient creates and validates an OpenSearch client connection.
func NewClient(baseUrl string) (*OpenSearchClient, error) {
	if err := handshake(baseUrl); err != nil {
		return nil, err
	}

	username, found := os.LookupEnv(envOpenSearchUsername)
	if !found {
		return nil, fmt.Errorf("%q env var is not set", envOpenSearchUsername)
	}
	password, found := os.LookupEnv(envOpenSearchPassword)
	if !found {
		return nil, fmt.Errorf("%q env var is not set", envOpenSearchPassword)
	}

	osClient, err := newOpenSearchClient(baseUrl, username, password)
	if err != nil {
		return nil, fmt.Errorf("error creating OpenSearch client: %w", err)
	}

	if err := checkServer(osClient); err != nil {
		return nil, err
	}

	return &OpenSearchClient{
		client:   osClient,
		username: username,
		password: password,
		url:      baseUrl,
	}, nil
}

// withProgress runs fn while displaying a progress spinner with the given message.
func withProgress(message string, fn func() error) error {
	stop := common.StartProgressSpinner(message)
	err := fn()
	stop()
	return err
}

// Init initializes the OpenSearch client by setting up models and pipelines.
// It creates or retrieves the model group, deploys models, and creates pipelines.
// The resolved model IDs are persisted to the snap config via cfg.
func (c *OpenSearchClient) Init(ctx context.Context) error {
	// Get or create the model group
	var modelGroupID string
	if err := withProgress("Creating model group", func() error {
		var err error
		modelGroupID, err = c.getOrCreateModelGroup(ctx)
		return err
	}); err != nil {
		return fmt.Errorf("error setting up model group: %w", err)
	}

	// Register and deploy the sentence transformer for embeddings
	if err := withProgress("Setting up embedding model", func() error {
		embeddingModelID, err := c.registerAndDeploySentenceTransformer(ctx, modelGroupID, "", "")
		if err != nil {
			return err
		}
		c.embeddingModelID = embeddingModelID
		fmt.Printf("\n run `sudo rag set --package %s=\"%s\"`\n", ConfEmbeddingModelID, embeddingModelID)
		return nil
	}); err != nil {
		return fmt.Errorf("error setting up embedding model: %w", err)
	}

	// Register and deploy the cross-encoder for reranking
	if err := withProgress("Setting up rerank model", func() error {
		rerankModelID, err := c.registerAndDeployCrossEncoder(ctx, modelGroupID, "", "")
		if err != nil {
			return err
		}
		c.rerankModelID = rerankModelID
		fmt.Printf("run `sudo rag set --package %s=\"%s\"`\n", ConfRerankModelID, rerankModelID)
		return nil
	}); err != nil {
		return fmt.Errorf("error setting up rerank model: %w", err)
	}

	// Create or update the ingest pipeline
	if err := withProgress("Setting up ingest pipeline", func() error {
		return c.getOrCreateIngestPipeline(ctx, c.embeddingModelID)
	}); err != nil {
		return fmt.Errorf("error setting up ingest pipeline: %w", err)
	}
	c.ingestPipeline = ingestPipelineName

	// Create or update the search pipeline
	if err := withProgress("Setting up search pipeline", func() error {
		return c.getOrCreateSearchPipeline(ctx, c.rerankModelID)
	}); err != nil {
		return fmt.Errorf("error setting up search pipeline: %w", err)
	}
	c.searchPipeline = searchPipelineName

	// Create or update the index template
	if err := withProgress("Setting up index template", func() error {
		return c.getOrCreateIndexTemplate(ctx)
	}); err != nil {
		return fmt.Errorf("error setting up index template: %w", err)
	}

	// Ensure the default index exists
	if err := withProgress("Setting up default index", func() error {
		return c.getOrCreateIndex(ctx, indexDefaultSubfix)
	}); err != nil {
		return fmt.Errorf("error setting up default index: %w", err)
	}

	// Ensure the sources metadata index exists
	if err := withProgress("Setting up sources metadata index", func() error {
		return c.getOrCreateSourcesIndex(ctx)
	}); err != nil {
		return fmt.Errorf("error setting up sources metadata index: %w", err)
	}

	return nil
}

func newOpenSearchClient(baseUrl, username, password string) (*opensearchapi.Client, error) {
	client, err := opensearchapi.NewClient(opensearchapi.Config{
		Client: opensearch.Config{
			Addresses: []string{baseUrl},
			Username:  username,
			Password:  password,
			Transport: &headerTransport{
				transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func handshake(baseURL string) error {
	stopProgress := common.StartProgressSpinner("Connecting to OpenSearch")
	defer stopProgress()

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		if parsedURL.Scheme == "https" {
			port = "443"
		} else {
			port = "9200"
		}
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("connection refused\n\n%s\n%s",
			common.SuggestServerStartup(),
			common.SuggestServerLogs())
	} else if err != nil {
		return err
	}
	conn.Close()

	return nil
}

func checkServer(client *opensearchapi.Client) error {
	stopProgress := common.StartProgressSpinner("Waiting for OpenSearch to be ready")
	defer stopProgress()

	const (
		retryInterval = 5 * time.Second
		waitTimeout   = 60 * time.Second
	)
	start := time.Now()
	for {
		resp, err := client.Cluster.Health(context.Background(), nil)
		if err != nil {
			if time.Since(start) > waitTimeout {
				return fmt.Errorf("opensearch not available\n\n%s\n%s",
					common.SuggestServerStartup(),
					common.SuggestServerLogs())
			}
			time.Sleep(retryInterval)
			continue
		}

		if resp.Inspect().Response.StatusCode == http.StatusOK {
			return nil
		}

		if time.Since(start) > waitTimeout {
			return fmt.Errorf("opensearch cluster not healthy\n\n%s\n%s",
				common.SuggestServerStartup(),
				common.SuggestServerLogs())
		}
		time.Sleep(retryInterval)
	}
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return t.transport.RoundTrip(req)
}

// AuthenticatedURL returns the base URL with credentials embedded, and the given
// index path appended. Used to pass credentials to external tools like elasticdump.
func (c *OpenSearchClient) AuthenticatedURL(indexPath string) string {
	parsed, err := url.Parse(c.url)
	if err != nil {
		return c.url + indexPath
	}
	parsed.User = url.UserPassword(c.username, c.password)
	parsed.Path = indexPath
	return parsed.String()
}

// IndexExists returns true if the given index exists in OpenSearch.
func (c *OpenSearchClient) IndexExists(ctx context.Context, indexName string) (bool, error) {
	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndicesExistsReq{
			Indices: []string{indexName},
		},
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("checking if index exists: %w", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// CountDocuments returns the number of documents in the given index.
// Returns 0 and no error if the index does not yet exist.
func (c *OpenSearchClient) CountDocuments(ctx context.Context, indexName string) (int, error) {
	path := fmt.Sprintf("/%s/_count", indexName)
	req, err := c.newAuthenticatedRequest(http.MethodGet, path, nil)
	if err != nil {
		return 0, fmt.Errorf("creating count request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("counting documents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("count documents failed with status %d: %s", resp.StatusCode, string(body))
	}

	var countResp struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&countResp); err != nil {
		return 0, fmt.Errorf("decoding count response: %w", err)
	}
	return countResp.Count, nil
}

// newAuthenticatedRequest creates an HTTP request with basic authentication.
func (c *OpenSearchClient) newAuthenticatedRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.url+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	return req, nil
}
