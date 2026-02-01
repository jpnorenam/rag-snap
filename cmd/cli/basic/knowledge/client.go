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

	"github.com/canonical/go-snapctl"
	"github.com/canonical/go-snapctl/env"
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

func Client(baseUrl string) error {
	if env.SnapInstanceName() != "" {
		serviceName := env.SnapInstanceName() + ".opensearch"
		services, err := snapctl.Services(serviceName).Run()
		if err != nil {
			return fmt.Errorf("error getting services: %v", err)
		}
		if services[serviceName].Current == "inactive" {
			return fmt.Errorf("opensearch not active\n\n%s",
				common.SuggestStartServer())
		}
	}

	if err := handshake(baseUrl); err != nil {
		return err
	}

	osClient, err := newOpenSearchClient(baseUrl)
	if err != nil {
		return fmt.Errorf("error creating OpenSearch client: %v", err)
	}

	if err := checkServer(osClient); err != nil {
		return err
	}

	opensearchUsername, _ := os.LookupEnv(envOpenSearchUsername)
	opensearchPassword, _ := os.LookupEnv(envOpenSearchPassword)

	client := &OpenSearchClient{
		client:   osClient,
		username: opensearchUsername,
		password: opensearchPassword,
		url:      baseUrl,
	}

	ctx := context.Background()
	if err := client.Init(ctx); err != nil {
		return fmt.Errorf("error initializing OpenSearch client: %v", err)
	}

	return nil
}

// Init initializes the OpenSearch client by setting up models and pipelines.
// It creates or retrieves the model group, deploys models, and creates pipelines.
func (c *OpenSearchClient) Init(ctx context.Context) error {
	// Get or create the model group
	modelGroupID, err := c.getOrCreateModelGroup(ctx)
	if err != nil {
		return fmt.Errorf("error setting up model group: %w", err)
	}

	// Register and deploy the sentence transformer for embeddings
	embeddingModelID, err := c.registerAndDeploySentenceTransformer(ctx, modelGroupID, "", "")
	if err != nil {
		return fmt.Errorf("error setting up embedding model: %w", err)
	}
	c.embeddingModelID = embeddingModelID

	// Register and deploy the cross-encoder for reranking
	rerankModelID, err := c.registerAndDeployCrossEncoder(ctx, modelGroupID, "", "")
	if err != nil {
		return fmt.Errorf("error setting up rerank model: %w", err)
	}
	c.rerankModelID = rerankModelID

	// Create or update the ingest pipeline
	if err := c.getOrCreateIngestPipeline(ctx, c.embeddingModelID); err != nil {
		return fmt.Errorf("error setting up ingest pipeline: %w", err)
	}
	c.ingestPipeline = ingestPipelineName

	// Create or update the search pipeline
	if err := c.getOrCreateSearchPipeline(ctx, c.rerankModelID); err != nil {
		return fmt.Errorf("error setting up search pipeline: %w", err)
	}
	c.searchPipeline = searchPipelineName

	// Create or update the index template
	if err := c.getOrCreateIndexTemplate(ctx); err != nil {
		return fmt.Errorf("error setting up index template: %w", err)
	}

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
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func handshake(baseUrl string) error {
	stopProgress := common.StartProgressSpinner("Connecting to OpenSearch")
	defer stopProgress()

	parsedURL, err := url.Parse(baseUrl)
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

// newAuthenticatedRequest creates an HTTP request with basic authentication.
func (c *OpenSearchClient) newAuthenticatedRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.url+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	return req, nil
}
