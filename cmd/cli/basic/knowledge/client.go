package knowledge

import (
	"context"
	"crypto/tls"
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

// headerTransport wraps an http.RoundTripper and adds default headers to all requests.
type headerTransport struct {
	transport http.RoundTripper
}

func Client(baseUrl string, init bool) error {
	fmt.Printf("Using opensearch cluster at %v\n", baseUrl)

	client, err := newClient(baseUrl)
	if err != nil {
		return err
	}

	if init {
		ctx := context.Background()
		if err := client.Init(ctx); err != nil {
			return fmt.Errorf("error initializing OpenSearch client: %v", err)
		}
	}

	return nil
}

func ListIndexes(baseUrl string) ([]IndexInfo, error) {
	client, err := newClient(baseUrl)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	return client.catIndexes(ctx)
}

func CreateIndex(baseUrl string, indexName string) error {
	client, err := newClient(baseUrl)
	if err != nil {
		return err
	}

	ctx := context.Background()
	return client.getOrCreateIndex(ctx, indexName)
}

// newClient creates and validates an OpenSearch client connection.
func newClient(baseUrl string) (*OpenSearchClient, error) {
	if err := handshake(baseUrl); err != nil {
		return nil, err
	}

	osClient, err := newOpenSearchClient(baseUrl)
	if err != nil {
		return nil, fmt.Errorf("error creating OpenSearch client: %v", err)
	}

	if err := checkServer(osClient); err != nil {
		return nil, err
	}

	opensearchUsername, _ := os.LookupEnv(envOpenSearchUsername)
	opensearchPassword, _ := os.LookupEnv(envOpenSearchPassword)

	return &OpenSearchClient{
		client:   osClient,
		username: opensearchUsername,
		password: opensearchPassword,
		url:      baseUrl,
	}, nil
}

// Init initializes the OpenSearch client by setting up models and pipelines.
// It creates or retrieves the model group, deploys models, and creates pipelines.
func (c *OpenSearchClient) Init(ctx context.Context) error {
	// Get or create the model group
	stopProgress := common.StartProgressSpinner("Creating model group")
	modelGroupID, err := c.getOrCreateModelGroup(ctx)
	if err != nil {
		stopProgress()
		return fmt.Errorf("error setting up model group: %w", err)
	}
	stopProgress()

	// Register and deploy the sentence transformer for embeddings
	stopProgress = common.StartProgressSpinner("Setting up embedding model")
	embeddingModelID, err := c.registerAndDeploySentenceTransformer(ctx, modelGroupID, "", "")
	if err != nil {
		stopProgress()
		return fmt.Errorf("error setting up embedding model: %w", err)
	}
	c.embeddingModelID = embeddingModelID
	stopProgress()

	// Register and deploy the cross-encoder for reranking
	stopProgress = common.StartProgressSpinner("Setting up rerank model")
	rerankModelID, err := c.registerAndDeployCrossEncoder(ctx, modelGroupID, "", "")
	if err != nil {
		stopProgress()
		return fmt.Errorf("error setting up rerank model: %w", err)
	}
	c.rerankModelID = rerankModelID
	stopProgress()

	// Create or update the ingest pipeline
	stopProgress = common.StartProgressSpinner("Setting up ingest pipeline")
	if err := c.getOrCreateIngestPipeline(ctx, c.embeddingModelID); err != nil {
		stopProgress()
		return fmt.Errorf("error setting up ingest pipeline: %w", err)
	}
	c.ingestPipeline = ingestPipelineName
	stopProgress()

	// Create or update the search pipeline
	stopProgress = common.StartProgressSpinner("Setting up search pipeline")
	if err := c.getOrCreateSearchPipeline(ctx, c.rerankModelID); err != nil {
		stopProgress()
		return fmt.Errorf("error setting up search pipeline: %w", err)
	}
	c.searchPipeline = searchPipelineName
	stopProgress()

	// Create or update the index template
	stopProgress = common.StartProgressSpinner("Setting up index template")
	if err := c.getOrCreateIndexTemplate(ctx); err != nil {
		stopProgress()
		return fmt.Errorf("error setting up index template: %w", err)
	}
	stopProgress()

	// Ensure the default index exists
	stopProgress = common.StartProgressSpinner("Setting up default index")
	if err := c.getOrCreateIndex(ctx, indexDefaultSubfix); err != nil {
		stopProgress()
		return fmt.Errorf("error setting up default index: %w", err)
	}
	stopProgress()

	return nil
}

func newOpenSearchClient(baseUrl string) (*opensearchapi.Client, error) {

	opensearchUsername, found := os.LookupEnv(envOpenSearchUsername)
	if !found {
		return nil, fmt.Errorf("%q env var is not set", envOpenSearchUsername)
	}

	opensearchPassword, found := os.LookupEnv(envOpenSearchPassword)
	if !found {
		return nil, fmt.Errorf("%q env var is not set", envOpenSearchPassword)
	}
	client, err := opensearchapi.NewClient(opensearchapi.Config{
		Client: opensearch.Config{
			Addresses: []string{baseUrl},
			Username:  opensearchUsername,
			Password:  opensearchPassword,
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

// newAuthenticatedRequest creates an HTTP request with basic authentication.
func (c *OpenSearchClient) newAuthenticatedRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.url+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	return req, nil
}
