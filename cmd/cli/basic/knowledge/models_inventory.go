package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Model roles, naming the config key that points at a model. A model with no
// role is not referenced by the engine — it is a candidate for pruning.
const (
	ModelRoleEmbedding = "embedding"
	ModelRoleRerank    = "rerank"
)

// ModelInfo describes one model registered in the snap's model group. SizeBytes
// and WorkerNodes are what make a stray model's cost visible: a deployed model is
// resident in memory on every worker node holding it, whether or not the engine
// still refers to it.
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	State       string `json:"state"`
	SizeBytes   int64  `json:"size_bytes"`
	WorkerNodes int    `json:"worker_nodes"`
	// Role is the engine role this model currently serves ("embedding",
	// "rerank"), or "" when nothing points at it.
	Role string `json:"role"`
}

// Deployed reports whether the model occupies memory on at least one node. It is
// true for partial and failed deployments too: those hold memory without being
// usable, which is exactly the case an operator needs to see.
func (m ModelInfo) Deployed() bool {
	switch m.State {
	case "DEPLOYED", "PARTIALLY_DEPLOYED", "DEPLOYING", "DEPLOY_FAILED":
		return true
	}
	return m.WorkerNodes > 0
}

// ModelRole labels a model by the engine role it serves, given the configured
// model IDs. Shared so the CLI (reading config directly) and the daemon report
// the same roles.
func ModelRole(id, embeddingModelID, rerankModelID string) string {
	switch id {
	case "":
		return ""
	case embeddingModelID:
		return ModelRoleEmbedding
	case rerankModelID:
		return ModelRoleRerank
	}
	return ""
}

// ListModels returns the models registered in the snap's model group, newest
// first is not guaranteed — the caller sorts. A missing model group is not an
// error: it means nothing has been initialized yet, so the inventory is empty.
// Roles are filled in from the given configured model IDs.
func (c *OpenSearchClient) ListModels(ctx context.Context, embeddingModelID, rerankModelID string) ([]ModelInfo, error) {
	modelGroupID, err := c.findModelGroup(ctx, modelGroupName)
	if err != nil {
		return nil, fmt.Errorf("error searching for model group: %w", err)
	}
	if modelGroupID == "" {
		return nil, nil
	}

	// Exclude chunk documents: the ML plugin stores a model's payload as extra
	// documents in the same index, and they would otherwise show up as models.
	// The payload fields are excluded from _source because a model chunk is
	// megabytes of base64 nobody here needs.
	searchBody := map[string]any{
		"size": 100,
		"query": map[string]any{
			"bool": map[string]any{
				"must":     []map[string]any{{"term": map[string]any{"model_group_id": modelGroupID}}},
				"must_not": []map[string]any{{"exists": map[string]any{"field": "chunk_number"}}},
			},
		},
		"_source": map[string]any{"excludes": []string{"content", "model_content"}},
	}

	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return nil, fmt.Errorf("error marshaling search query: %w", err)
	}

	req, err := c.newAuthenticatedRequest(http.MethodGet, "/_plugins/_ml/models/_search", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("error listing models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("model search failed with status %d: %s", resp.StatusCode, string(body))
	}

	var searchResp modelInventoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("error decoding model search response: %w", err)
	}

	models := make([]ModelInfo, 0, len(searchResp.Hits.Hits))
	for _, hit := range searchResp.Hits.Hits {
		if hit.Source.Name == "" { // defensive: a chunk doc that slipped the filter
			continue
		}
		models = append(models, ModelInfo{
			ID:          hit.ID,
			Name:        hit.Source.Name,
			Version:     hit.Source.ModelVersion,
			State:       hit.Source.ModelState,
			SizeBytes:   hit.Source.ModelContentSizeInBytes,
			WorkerNodes: hit.Source.CurrentWorkerNodeCount,
			Role:        ModelRole(hit.ID, embeddingModelID, rerankModelID),
		})
	}

	return models, nil
}

// UndeployModel releases a model from the ML nodes' memory, leaving it
// registered so it can be deployed again without re-downloading it.
func (c *OpenSearchClient) UndeployModel(ctx context.Context, modelID string) error {
	req, err := c.newAuthenticatedRequest(http.MethodPost, fmt.Sprintf("/_plugins/_ml/models/%s/_undeploy", modelID), nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error undeploying model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("undeploy failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// DeleteModel removes a model from OpenSearch, undeploying it first so the
// delete is not refused for a model that is still resident. An undeploy failure
// is not fatal: a model that was never deployed cannot be undeployed, and the
// delete below is the operation the caller asked for.
func (c *OpenSearchClient) DeleteModel(ctx context.Context, modelID string) error {
	_ = c.UndeployModel(ctx, modelID)

	req, err := c.newAuthenticatedRequest(http.MethodDelete, fmt.Sprintf("/_plugins/_ml/models/%s", modelID), nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error deleting model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("model %s not found", modelID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// modelInventoryResponse decodes the model search hits the inventory needs.
type modelInventoryResponse struct {
	Hits struct {
		Hits []struct {
			ID     string `json:"_id"`
			Source struct {
				Name                    string `json:"name"`
				ModelVersion            string `json:"model_version"`
				ModelState              string `json:"model_state"`
				ModelContentSizeInBytes int64  `json:"model_content_size_in_bytes"`
				CurrentWorkerNodeCount  int    `json:"current_worker_node_count"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}
