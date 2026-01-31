package knowledge

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/canonical/go-snapctl"
	"github.com/canonical/go-snapctl/env"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/chzyer/readline"
	"github.com/fatih/color"
	opensearch "github.com/opensearch-project/opensearch-go/v4"
	opensearchapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

type OpenSearchClient struct {
	client           *opensearchapi.Client
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

	fmt.Printf("Using OpenSearch at %v\n", baseUrl)

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

	if indexName == "" {
		indexName = "rag-snap-default"
	}
	if verbose {
		fmt.Printf("Using index %v\n", indexName)
	}

	client := &OpenSearchClient{
		client: osClient,
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
	client, err := opensearchapi.NewClient(opensearchapi.Config{
		Client: opensearch.Config{
			Addresses: []string{baseUrl},
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
	return client, n
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

		if resp.StatusCode == http.StatusOK {
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

func (c *OpenSearchClient) search(query string) error {
	searchBody := map[string]interface{}{
		"_source": map[string]interface{}{
			"excludes": []string{"embedding"},
		},
		"query": map[string]interface{}{
			"neural": map[string]interface{}{
				"embedding": map[string]interface{}{
					"query_text": query,
					"model_id":   c.embeddingModelID,
					"k":          10,
				},
			},
		},
		"ext": map[string]interface{}{
			"rerank": map[string]interface{}{
				"query_context": map[string]interface{}{
					"query_text": query,
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return fmt.Errorf("error marshaling search query: %v", err)
	}

	if c.verbose {
		fmt.Printf("Sending search request: %s\n", string(bodyBytes))
	}

	stopProgress := common.StartProgressSpinner("Searching")
	resp, err := c.client.Search(
		context.Background(),
		&opensearchapi.SearchReq{
			Indices: []string{c.indexName},
			Body:    bytes.NewReader(bodyBytes),
		},
	)
	stopProgress()

	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return fmt.Errorf("connection refused\n\n%s",
				common.SuggestServerLogs())
		}
		return fmt.Errorf("search error: %v\n\n%s", err,
			common.SuggestServerLogs())
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("search failed with status %d", resp.StatusCode)
	}

	return c.displayResults(resp)
}

func (c *OpenSearchClient) displayResults(resp *opensearchapi.SearchResp) error {
	totalHits := resp.Hits.Total.Value
	fmt.Printf("\n%s %d results\n\n", color.GreenString("Found"), totalHits)

	if totalHits == 0 {
		return nil
	}

	for i, hit := range resp.Hits.Hits {
		fmt.Printf("%s %d\n", color.YellowString("Result"), i+1)
		fmt.Printf("  Index: %s\n", hit.Index)
		fmt.Printf("  ID: %s\n", hit.ID)
		fmt.Printf("  Score: %.4f\n", hit.Score)

		if hit.Source != nil {
			var source map[string]interface{}
			if err := json.Unmarshal(hit.Source, &source); err == nil {
				for key, value := range source {
					fmt.Printf("  %s: %v\n", key, value)
				}
			}
		}
		fmt.Println()
	}

	return nil
}

func filterInput(r rune) (rune, bool) {
	switch r {
	case readline.CharCtrlZ:
		return r, false
	}
	return r, true
}


