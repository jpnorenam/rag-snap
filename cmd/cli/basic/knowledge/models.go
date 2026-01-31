package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	opensearchapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

const (
	modelGroupName = "rag-snap-models"
)

// getOrCreateModelGroup searches for a model group named "rag-snap-models".
// If it exists, returns the model_group_id. If not, creates one and returns the new model_group_id.
func (c *OpenSearchClient) getOrCreateModelGroup(ctx context.Context) (string, error) {
	modelGroupID, err := c.findModelGroup(ctx, modelGroupName)
	if err != nil {
		return "", fmt.Errorf("error searching for model group: %w", err)
	}

	if modelGroupID != "" {
		return modelGroupID, nil
	}

	// Model group doesn't exist, create it
	modelGroupID, err = c.createModelGroup(ctx, modelGroupName)
	if err != nil {
		return "", fmt.Errorf("error creating model group: %w", err)
	}

	return modelGroupID, nil
}

// findModelGroup searches for a model group by name and returns its ID if found.
func (c *OpenSearchClient) findModelGroup(ctx context.Context, name string) (string, error) {
	searchBody := map[string]interface{}{
		"query": map[string]interface{}{
			"match": map[string]interface{}{
				"name": name,
			},
		},
	}

	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling search query: %w", err)
	}

	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.Request{
			Method: http.MethodGet,
			Path:   "/_plugins/_ml/model_groups/_search",
			Body:   bytes.NewReader(bodyBytes),
		},
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("error executing search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var searchResp modelGroupSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return "", fmt.Errorf("error decoding search response: %w", err)
	}

	if searchResp.Hits.Total.Value > 0 {
		for _, hit := range searchResp.Hits.Hits {
			if hit.Source.Name == name {
				return hit.ID, nil
			}
		}
	}

	return "", nil
}

// createModelGroup creates a new model group with the given name and returns its ID.
func (c *OpenSearchClient) createModelGroup(ctx context.Context, name string) (string, error) {
	requestBody := map[string]interface{}{
		"name":        name,
		"description": "Model group for RAG snap models",
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request body: %w", err)
	}

	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.Request{
			Method: http.MethodPost,
			Path:   "/_plugins/_ml/model_groups/_register",
			Body:   bytes.NewReader(bodyBytes),
		},
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("error executing register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("register request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var registerResp modelGroupRegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&registerResp); err != nil {
		return "", fmt.Errorf("error decoding register response: %w", err)
	}

	if registerResp.ModelGroupID == "" {
		return "", fmt.Errorf("model group created but no ID returned")
	}

	return registerResp.ModelGroupID, nil
}

// Response types for OpenSearch ML API

type modelGroupSearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []struct {
			ID     string `json:"_id"`
			Source struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type modelGroupRegisterResponse struct {
	ModelGroupID string `json:"model_group_id"`
	Status       string `json:"status"`
}

const (
	defaultSentenceTransformerName    = "huggingface/sentence-transformers/msmarco-distilbert-base-tas-b"
	defaultSentenceTransformerVersion = "1.0.2"

	defaultCrossEncoderName    = "huggingface/cross-encoders/ms-marco-MiniLM-L-12-v2"
	defaultCrossEncoderVersion = "1.0.2"
)

// registerAndDeploySentenceTransformer registers and deploys a TORCH_SCRIPT sentence transformer model.
// If modelName or modelVersion are empty, uses the default model.
// If the model is already deployed in the model group, returns the existing model ID.
func (c *OpenSearchClient) registerAndDeploySentenceTransformer(ctx context.Context, modelGroupID, modelName, modelVersion string) (string, error) {
	if modelName == "" {
		modelName = defaultSentenceTransformerName
	}
	if modelVersion == "" {
		modelVersion = defaultSentenceTransformerVersion
	}

	// Check if model already exists in the model group
	existingModelID, err := c.findModelInGroup(ctx, modelGroupID, modelName, modelVersion)
	if err != nil {
		return "", fmt.Errorf("error checking for existing model: %w", err)
	}
	if existingModelID != "" {
		// Model already exists, check if it's deployed
		state, err := c.getModelState(ctx, existingModelID)
		if err != nil {
			return "", fmt.Errorf("error getting model state: %w", err)
		}
		if state == "DEPLOYED" {
			return existingModelID, nil
		}
		// Model exists but not deployed, deploy it
		if state == "REGISTERED" {
			if err := c.deployModel(ctx, existingModelID); err != nil {
				return "", fmt.Errorf("error deploying existing model: %w", err)
			}
			if err := c.waitForModelState(ctx, existingModelID, "DEPLOYED"); err != nil {
				return "", fmt.Errorf("error waiting for model deployment: %w", err)
			}
		}
		return existingModelID, nil
	}

	// Register the model
	modelID, err := c.registerModel(ctx, modelGroupID, modelName, modelVersion, "TORCH_SCRIPT", "TEXT_EMBEDDING")
	if err != nil {
		return "", fmt.Errorf("error registering sentence transformer model: %w", err)
	}

	// Wait for registration to complete
	if err := c.waitForModelState(ctx, modelID, "REGISTERED"); err != nil {
		return "", fmt.Errorf("error waiting for model registration: %w", err)
	}

	// Deploy the model
	if err := c.deployModel(ctx, modelID); err != nil {
		return "", fmt.Errorf("error deploying sentence transformer model: %w", err)
	}

	// Wait for deployment to complete
	if err := c.waitForModelState(ctx, modelID, "DEPLOYED"); err != nil {
		return "", fmt.Errorf("error waiting for model deployment: %w", err)
	}

	return modelID, nil
}

// registerAndDeployCrossEncoder registers and deploys a TORCH_SCRIPT cross-encoder model.
// If modelName or modelVersion are empty, uses the default model.
// If the model is already deployed in the model group, returns the existing model ID.
func (c *OpenSearchClient) registerAndDeployCrossEncoder(ctx context.Context, modelGroupID, modelName, modelVersion string) (string, error) {
	if modelName == "" {
		modelName = defaultCrossEncoderName
	}
	if modelVersion == "" {
		modelVersion = defaultCrossEncoderVersion
	}

	// Check if model already exists in the model group
	existingModelID, err := c.findModelInGroup(ctx, modelGroupID, modelName, modelVersion)
	if err != nil {
		return "", fmt.Errorf("error checking for existing model: %w", err)
	}
	if existingModelID != "" {
		// Model already exists, check if it's deployed
		state, err := c.getModelState(ctx, existingModelID)
		if err != nil {
			return "", fmt.Errorf("error getting model state: %w", err)
		}
		if state == "DEPLOYED" {
			return existingModelID, nil
		}
		// Model exists but not deployed, deploy it
		if state == "REGISTERED" {
			if err := c.deployModel(ctx, existingModelID); err != nil {
				return "", fmt.Errorf("error deploying existing model: %w", err)
			}
			if err := c.waitForModelState(ctx, existingModelID, "DEPLOYED"); err != nil {
				return "", fmt.Errorf("error waiting for model deployment: %w", err)
			}
		}
		return existingModelID, nil
	}

	// Register the model
	modelID, err := c.registerModel(ctx, modelGroupID, modelName, modelVersion, "TORCH_SCRIPT", "TEXT_SIMILARITY")
	if err != nil {
		return "", fmt.Errorf("error registering cross-encoder model: %w", err)
	}

	// Wait for registration to complete
	if err := c.waitForModelState(ctx, modelID, "REGISTERED"); err != nil {
		return "", fmt.Errorf("error waiting for model registration: %w", err)
	}

	// Deploy the model
	if err := c.deployModel(ctx, modelID); err != nil {
		return "", fmt.Errorf("error deploying cross-encoder model: %w", err)
	}

	// Wait for deployment to complete
	if err := c.waitForModelState(ctx, modelID, "DEPLOYED"); err != nil {
		return "", fmt.Errorf("error waiting for model deployment: %w", err)
	}

	return modelID, nil
}

// findModelInGroup searches for a model by name and version within a model group.
// Returns the model ID if found, empty string if not found.
func (c *OpenSearchClient) findModelInGroup(ctx context.Context, modelGroupID, modelName, modelVersion string) (string, error) {
	searchBody := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []map[string]interface{}{
					{
						"term": map[string]interface{}{
							"model_group_id": modelGroupID,
						},
					},
					{
						"term": map[string]interface{}{
							"name.keyword": modelName,
						},
					},
					{
						"term": map[string]interface{}{
							"model_version": modelVersion,
						},
					},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(searchBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling search query: %w", err)
	}

	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.Request{
			Method: http.MethodGet,
			Path:   "/_plugins/_ml/models/_search",
			Body:   bytes.NewReader(bodyBytes),
		},
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("error executing search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var searchResp modelSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return "", fmt.Errorf("error decoding search response: %w", err)
	}

	if searchResp.Hits.Total.Value > 0 && len(searchResp.Hits.Hits) > 0 {
		return searchResp.Hits.Hits[0].ID, nil
	}

	return "", nil
}

// getModelState retrieves the current state of a model.
func (c *OpenSearchClient) getModelState(ctx context.Context, modelID string) (string, error) {
	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.Request{
			Method: http.MethodGet,
			Path:   fmt.Sprintf("/_plugins/_ml/models/%s", modelID),
		},
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("error getting model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get model request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var modelResp modelStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		return "", fmt.Errorf("error decoding model response: %w", err)
	}

	return modelResp.ModelState, nil
}

// registerModel registers a model with OpenSearch ML plugin.
func (c *OpenSearchClient) registerModel(ctx context.Context, modelGroupID, modelName, modelVersion, modelFormat, functionName string) (string, error) {
	requestBody := map[string]interface{}{
		"name":           modelName,
		"version":        modelVersion,
		"model_group_id": modelGroupID,
		"model_format":   modelFormat,
		"function_name":  functionName,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling request body: %w", err)
	}

	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.Request{
			Method: http.MethodPost,
			Path:   "/_plugins/_ml/models/_register",
			Body:   bytes.NewReader(bodyBytes),
		},
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("error executing register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("register request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var registerResp modelRegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&registerResp); err != nil {
		return "", fmt.Errorf("error decoding register response: %w", err)
	}

	// Registration is async, we get a task_id. We need to poll for the model_id.
	if registerResp.ModelID != "" {
		return registerResp.ModelID, nil
	}

	if registerResp.TaskID != "" {
		return c.waitForTaskAndGetModelID(ctx, registerResp.TaskID)
	}

	return "", fmt.Errorf("no model_id or task_id returned from registration")
}

// deployModel deploys a registered model.
func (c *OpenSearchClient) deployModel(ctx context.Context, modelID string) error {
	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.Request{
			Method: http.MethodPost,
			Path:   fmt.Sprintf("/_plugins/_ml/models/%s/_deploy", modelID),
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error executing deploy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deploy request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// waitForTaskAndGetModelID polls a task until it completes and returns the model_id.
func (c *OpenSearchClient) waitForTaskAndGetModelID(ctx context.Context, taskID string) (string, error) {
	const (
		pollInterval = 2 * time.Second
		timeout      = 5 * time.Minute
	)

	start := time.Now()
	for {
		if time.Since(start) > timeout {
			return "", fmt.Errorf("timeout waiting for task %s to complete", taskID)
		}

		resp, err := c.client.Client.Do(
			ctx,
			opensearchapi.Request{
				Method: http.MethodGet,
				Path:   fmt.Sprintf("/_plugins/_ml/tasks/%s", taskID),
			},
			nil,
		)
		if err != nil {
			return "", fmt.Errorf("error getting task status: %w", err)
		}

		var taskResp taskStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("error decoding task response: %w", err)
		}
		resp.Body.Close()

		switch taskResp.State {
		case "COMPLETED":
			if taskResp.ModelID != "" {
				return taskResp.ModelID, nil
			}
			return "", fmt.Errorf("task completed but no model_id returned")
		case "FAILED":
			return "", fmt.Errorf("task failed: %s", taskResp.Error)
		}

		time.Sleep(pollInterval)
	}
}

// waitForModelState polls the model status until it reaches the desired state.
func (c *OpenSearchClient) waitForModelState(ctx context.Context, modelID, desiredState string) error {
	const (
		pollInterval = 2 * time.Second
		timeout      = 5 * time.Minute
	)

	start := time.Now()
	for {
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for model %s to reach state %s", modelID, desiredState)
		}

		resp, err := c.client.Client.Do(
			ctx,
			opensearchapi.Request{
				Method: http.MethodGet,
				Path:   fmt.Sprintf("/_plugins/_ml/models/%s", modelID),
			},
			nil,
		)
		if err != nil {
			return fmt.Errorf("error getting model status: %w", err)
		}

		var modelResp modelStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
			resp.Body.Close()
			return fmt.Errorf("error decoding model response: %w", err)
		}
		resp.Body.Close()

		if modelResp.ModelState == desiredState {
			return nil
		}

		if modelResp.ModelState == "DEPLOY_FAILED" || modelResp.ModelState == "REGISTER_FAILED" {
			return fmt.Errorf("model reached failed state: %s", modelResp.ModelState)
		}

		time.Sleep(pollInterval)
	}
}

type modelRegisterResponse struct {
	TaskID  string `json:"task_id"`
	ModelID string `json:"model_id"`
	Status  string `json:"status"`
}

type modelSearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []struct {
			ID     string `json:"_id"`
			Source struct {
				Name         string `json:"name"`
				ModelVersion string `json:"model_version"`
				ModelGroupID string `json:"model_group_id"`
				ModelState   string `json:"model_state"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type taskStatusResponse struct {
	ModelID string `json:"model_id"`
	State   string `json:"state"`
	Error   string `json:"error"`
}

type modelStatusResponse struct {
	ModelID    string `json:"model_id"`
	ModelState string `json:"model_state"`
}
